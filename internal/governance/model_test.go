package governance

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The contract's test vectors (contract/vectors), run against AOS's implementation.

func readVectors(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("contract", "vectors", name))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func vectorModel(t *testing.T, spec map[string]any) any {
	t.Helper()
	if file, ok := spec["file"].(string); ok {
		data, err := os.ReadFile(filepath.Join("contract", "vectors", file))
		if err != nil {
			t.Fatal(err)
		}
		var raw any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	return spec["inline"]
}

// mergePatch applies an RFC 7386 JSON Merge Patch: objects merge, null removes, else replaces.
func mergePatch(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	result := map[string]any{}
	if t, ok := target.(map[string]any); ok {
		for k, v := range t {
			result[k] = v
		}
	}
	for k, v := range p {
		if v == nil {
			delete(result, k)
		} else {
			result[k] = mergePatch(result[k], v)
		}
	}
	return result
}

func TestResolutionVectors(t *testing.T) {
	data := readVectors(t, "resolution.json")
	models := map[string]*Model{}
	for name, spec := range data["models"].(map[string]any) {
		m, err := NewModel(vectorModel(t, spec.(map[string]any)))
		if err != nil {
			t.Fatalf("model %q: %v", name, err)
		}
		models[name] = m
	}
	for _, c := range data["cases"].([]any) {
		tc := c.(map[string]any)
		t.Run(tc["name"].(string), func(t *testing.T) {
			got := models[tc["model"].(string)].Resolve(tc["claims"].(map[string]any), tc["app"].(string))
			want := []string{}
			for _, f := range tc["expect"].([]any) {
				want = append(want, f.(string))
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestValidationVectors(t *testing.T) {
	data := readVectors(t, "validation.json")
	for _, c := range data["cases"].([]any) {
		tc := c.(map[string]any)
		t.Run(tc["name"].(string), func(t *testing.T) {
			var raw any
			if spec, ok := tc["model"].(map[string]any); ok {
				raw = vectorModel(t, spec)
			} else {
				raw = mergePatch(data["base"], tc["patch"])
			}
			_, err := NewModel(raw)
			if tc["valid"] == true {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			var me *ModelError
			if !errors.As(err, &me) || me.Code != tc["error"] {
				t.Fatalf("want %v, got %v", tc["error"], err)
			}
		})
	}
}

func TestDefaultModelLoads(t *testing.T) {
	if _, err := LoadModel(""); err != nil {
		t.Fatal(err)
	}
}

// The contracts are copies synced from prudence-governance; an edit made here fails this test.
func TestContractCopyIsUnmodified(t *testing.T) {
	for _, dir := range []string{"contract", "decision-contract"} {
		t.Run(dir, func(t *testing.T) { checkSyncedCopy(t, dir) })
	}
}

func checkSyncedCopy(t *testing.T, dir string) {
	f, err := os.Open(filepath.Join(dir, "SOURCE"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checked := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		sum, name, ok := strings.Cut(line, "  ")
		if !ok {
			t.Fatalf("bad SOURCE line %q", line)
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s/%s differs from the synced copy: change the contract in prudence-governance and run its sync.sh", dir, name)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("SOURCE lists no files")
	}
}

package main
import (
	"encoding/json"
	"fmt"
	"time"
)
type Event struct {
	Time     time.Time
	Agent    string
	Kind     string
	Message  string
	Duration string
	Data     string
}
func main() {
	e := Event{Time: time.Now(), Agent: "Verifier", Kind: "await_human", Message: "sre", Data: `{"hello":"world"}`}
	b, _ := json.Marshal(e)
	fmt.Println(string(b))
}

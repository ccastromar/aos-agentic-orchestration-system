package agent

// stepOutputsKey keeps each step's output under its step id, so two steps that call the same
// tool keep both outputs. Later steps reference them as {{ index . "steps.<id>.<key>" }}; the
// tool-named keys stay for the pipelines that already reference those.
const stepOutputsKey = "_steps"

// recordStepOutput stores out under the step's id and publishes it as steps.<id>.<key>.
// A step without an id has no stable identity and only keeps its tool-named key.
func recordStepOutput(stepResults map[string]any, vars map[string]any, stepID string, out map[string]any) {
	if stepID == "" {
		return
	}
	byStep, _ := stepResults[stepOutputsKey].(map[string]any)
	if byStep == nil {
		byStep = map[string]any{}
		stepResults[stepOutputsKey] = byStep
	}
	byStep[stepID] = out
	for k, v := range out {
		vars["steps."+stepID+"."+k] = v
	}
}

// restoreResultVars rebuilds the result variables of the steps already run, as when an
// execution resumes after a human gate or a callback.
func restoreResultVars(stepResults map[string]any, vars map[string]any) {
	for k, val := range stepResults {
		resMap, ok := val.(map[string]any)
		if !ok {
			continue
		}
		if k == stepOutputsKey {
			for stepID, out := range resMap {
				if outMap, ok := out.(map[string]any); ok {
					for mk, mv := range outMap {
						vars["steps."+stepID+"."+mk] = mv
					}
				}
			}
			continue
		}
		for mk, mv := range resMap {
			vars[k+"."+mk] = mv
		}
	}
}

// publicResults is what the Analyst summarises and the task returns: the per-step copies stay
// in the execution state only, so the result does not show each output twice.
func publicResults(stepResults map[string]any) map[string]any {
	out := cloneMap(stepResults)
	delete(out, stepOutputsKey)
	return out
}

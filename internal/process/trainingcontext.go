package process

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/corvines/outrider/internal/endpoint"
	"github.com/corvines/outrider/internal/manifest"
)

// noteTrainingContext compares the training context a profile declares against
// the one the loaded model reports, and writes the disagreement to the server
// log. Validation bounds the requested context by the declared value and
// nothing checks the declared value itself, so a profile that under-declares
// caps what it is allowed to ask for and no other route notices.
//
// It reports, it does not enforce: the backend's number can be wrong too, and
// refusing to start over a metadata mismatch is worse than a line in the log.
func noteTrainingContext(ctx context.Context, plan manifest.Plan) {
	lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resolved, err := endpoint.FetchResolved(lookup, plan.Endpoint, plan.Profile.ID)
	if err != nil {
		return
	}
	note := trainingContextNote(plan.Profile.ID, plan.Profile.Context.Original, resolved.TrainingContext)
	if note == "" {
		return
	}
	appendLogLine(plan.State.Log, note)
}

// trainingContextNote returns the line to log, or "" when there is nothing to
// say. A backend that reports no training context has not answered the
// question, which is not the same as agreeing.
func trainingContextNote(profileID string, declared int, reported int) string {
	if reported <= 0 || declared == reported {
		return ""
	}
	shape := "declares a larger training context than the loaded model reports"
	if declared < reported {
		shape = "declares a smaller training context than the loaded model reports," +
			" so it caps its own context lower than the model allows"
	}
	return fmt.Sprintf(
		"outrider: %s %s: declared %d, reported %d",
		profileID, shape, declared, reported,
	)
}

func appendLogLine(path string, line string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintln(file, line)
}

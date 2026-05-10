package metadata

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"videobatch/internal/config"
	"videobatch/internal/recipe"
	"videobatch/internal/workerpool"
)

func TestProcessFullModeRunsCleanThenDiversify(t *testing.T) {
	dir := t.TempDir()
	rendered := dir + "/rendered.mp4"
	output := dir + "/out/final.mp4"
	if err := os.WriteFile(rendered, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	runner := Runner{ExifToolPath: "/bin/exiftool", RunCommand: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte("ok"), nil
	}}
	rec := &recipe.Recipe{Metadata: recipe.Metadata{Mode: "clean_diversify", Clean: true, Diversify: []recipe.MetadataTag{{Name: "Software", Value: "videobatch-test"}, {Name: "Comment", Value: "recipe-test"}}}}
	err := runner.Process(context.Background(), config.Config{}, workerpool.Job{OutputPath: output}, rec, rendered)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected clean and diversify calls, got %d", len(calls))
	}
	wantClean := []string{"/bin/exiftool", "-overwrite_original", "-all=", output}
	if !reflect.DeepEqual(calls[0], wantClean) {
		t.Fatalf("clean call = %#v, want %#v", calls[0], wantClean)
	}
	if strings.Join(calls[1], " ") != "/bin/exiftool -overwrite_original -Software=videobatch-test -Comment=recipe-test "+output {
		t.Fatalf("unexpected diversify call: %#v", calls[1])
	}
}

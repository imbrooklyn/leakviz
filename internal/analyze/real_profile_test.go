package analyze

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/imbrooklyn/leakviz/internal/profile"
)

const realProfileTestDeadline = 45 * time.Second

var realProfileRetryBackoff = [...]time.Duration{
	0,
	10 * time.Millisecond,
	40 * time.Millisecond,
	160 * time.Millisecond,
}

func TestRealGo127LeakProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real goroutineleak subprocesses in short mode")
	}
	if !strings.HasPrefix(runtime.Version(), "go1.27") {
		t.Fatalf("runtime version = %q, want Go 1.27 for frozen profile evidence", runtime.Version())
	}

	contextWithDeadline, cancel := context.WithTimeout(context.Background(), realProfileTestDeadline)
	defer cancel()

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	temporaryDirectory := t.TempDir()

	tests := []struct {
		name          string
		program       string
		assertProfile func(Analysis) (Group, error)
	}{
		{
			name:          "channel receive labels and inline frame",
			program:       "chanrecv",
			assertProfile: assertRealChannelReceive,
		},
		{
			name:          "wait group",
			program:       "syncwait",
			assertProfile: assertRealWaitGroup,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binaryPath := filepath.Join(temporaryDirectory, test.program)
			programPath := "./" + filepath.Join("testdata", "programs", test.program)
			if err := buildRealLeakProgram(contextWithDeadline, repositoryRoot, programPath, binaryPath); err != nil {
				t.Fatal(err)
			}

			group, attempt, err := captureRealLeakProfile(
				contextWithDeadline,
				temporaryDirectory,
				binaryPath,
				test.program,
				test.assertProfile,
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf(
				"real profile evidence: program=%s attempt=%d count=%d blocker=%s labels=%v stack=%s",
				test.program,
				attempt,
				group.Count,
				group.Blocker.Kind,
				group.Labels,
				formatRealStack(group.Stack),
			)
		})
	}
}

func buildRealLeakProgram(ctx context.Context, repositoryRoot, programPath, binaryPath string) error {
	command := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, programPath)
	command.Dir = repositoryRoot
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build real leak program %s: %w\n%s", programPath, err, output)
	}
	return nil
}

func captureRealLeakProfile(
	ctx context.Context,
	temporaryDirectory string,
	binaryPath string,
	program string,
	assertProfile func(Analysis) (Group, error),
) (Group, int, error) {
	var attemptErrors []error
	for attemptIndex, backoff := range realProfileRetryBackoff {
		if err := waitForRealProfileRetry(ctx, backoff); err != nil {
			return Group{}, 0, err
		}

		profilePath := filepath.Join(
			temporaryDirectory,
			fmt.Sprintf("%s-attempt-%d.pprof", program, attemptIndex+1),
		)
		if err := runRealLeakProgram(ctx, binaryPath, profilePath); err != nil {
			attemptErrors = append(attemptErrors, fmt.Errorf("attempt %d: %w", attemptIndex+1, err))
			continue
		}

		file, err := os.Open(profilePath)
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Errorf("attempt %d: open profile: %w", attemptIndex+1, err))
			continue
		}
		snapshot, parseErr := profile.Parse(program+".pprof", file)
		closeErr := file.Close()
		if parseErr != nil {
			attemptErrors = append(attemptErrors, fmt.Errorf("attempt %d: parse profile: %w", attemptIndex+1, parseErr))
			continue
		}
		if closeErr != nil {
			attemptErrors = append(attemptErrors, fmt.Errorf("attempt %d: close profile: %w", attemptIndex+1, closeErr))
			continue
		}

		analysis, err := Analyze(snapshot, Options{})
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Errorf("attempt %d: analyze profile: %w", attemptIndex+1, err))
			continue
		}
		group, err := assertProfile(analysis)
		if err != nil {
			attemptErrors = append(attemptErrors, fmt.Errorf("attempt %d: %w", attemptIndex+1, err))
			continue
		}
		return group, attemptIndex + 1, nil
	}

	return Group{}, 0, fmt.Errorf(
		"real leak profile %s failed after %d bounded attempts: %w",
		program,
		len(realProfileRetryBackoff),
		errors.Join(attemptErrors...),
	)
}

func waitForRealProfileRetry(ctx context.Context, backoff time.Duration) error {
	if backoff == 0 {
		return nil
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("real profile deadline: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func runRealLeakProgram(ctx context.Context, binaryPath, profilePath string) error {
	command := exec.CommandContext(ctx, binaryPath, profilePath)
	command.Env = append(os.Environ(), "GOMAXPROCS=1")

	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("open child stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open child stdout: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("start child: %w", err)
	}

	ready := make(chan error, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadString('\n')
		if readErr != nil {
			ready <- fmt.Errorf("read ready handshake: %w", readErr)
			return
		}
		if strings.TrimSpace(line) != "READY" {
			ready <- fmt.Errorf("unexpected ready handshake %q", strings.TrimSpace(line))
			return
		}
		ready <- nil
	}()

	select {
	case <-ctx.Done():
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("wait for ready handshake: %w", ctx.Err())
	case err := <-ready:
		if err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("%w; stderr=%q", err, stderr.String())
		}
	}

	if _, err := io.WriteString(stdin, "capture\n"); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("send capture command: %w", err)
	}
	if err := stdin.Close(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("close child stdin: %w", err)
	}
	if err := command.Wait(); err != nil {
		return fmt.Errorf("wait for child: %w; stderr=%q", err, stderr.String())
	}
	return nil
}

func assertRealChannelReceive(analysis Analysis) (Group, error) {
	group, err := realGroupWithFunction(analysis, ".receiveInline")
	if err != nil {
		return Group{}, err
	}
	if group.Count < 1 {
		return Group{}, fmt.Errorf("channel receive count = %d, want positive", group.Count)
	}
	if group.Blocker.Kind != BlockerChanReceive {
		return Group{}, fmt.Errorf("channel receive blocker = %q, want %q", group.Blocker.Kind, BlockerChanReceive)
	}
	if len(group.Stack) == 0 || group.Stack[0].Function != "runtime.gopark" {
		return Group{}, fmt.Errorf("channel receive stack is not leaf-to-root: %#v", group.Stack)
	}

	inlineIndex := realFrameIndex(group.Stack, ".receiveInline")
	callerIndex := realFrameIndex(group.Stack, ".leakChannelReceive")
	if inlineIndex < 0 || !group.Stack[inlineIndex].Inlined {
		return Group{}, fmt.Errorf("receiveInline frame is absent or not marked inline: %#v", group.Stack)
	}
	if callerIndex < 0 || inlineIndex >= callerIndex {
		return Group{}, fmt.Errorf("inline/caller frame order is not leaf-to-root: %#v", group.Stack)
	}
	if !realLabelHasValue(group.Labels, "scenario", "channel_receive", group.Count) {
		return Group{}, fmt.Errorf("scenario label is absent: %#v", group.Labels)
	}
	if !realLabelHasValue(group.Labels, "tenant", "real-inline", group.Count) {
		return Group{}, fmt.Errorf("tenant label is absent: %#v", group.Labels)
	}
	return group, nil
}

func assertRealWaitGroup(analysis Analysis) (Group, error) {
	group, err := realGroupWithFunction(analysis, ".leakWaitGroup")
	if err != nil {
		return Group{}, err
	}
	if group.Count < 1 {
		return Group{}, fmt.Errorf("wait group count = %d, want positive", group.Count)
	}
	if group.Blocker.Kind != BlockerWaitGroup {
		return Group{}, fmt.Errorf("wait group blocker = %q, want %q", group.Blocker.Kind, BlockerWaitGroup)
	}
	if realFrameIndex(group.Stack, "sync.(*WaitGroup).Wait") < 0 {
		return Group{}, fmt.Errorf("wait group evidence frame is absent: %#v", group.Stack)
	}
	return group, nil
}

func realGroupWithFunction(analysis Analysis, functionSuffix string) (Group, error) {
	for _, group := range analysis.Groups {
		if realFrameIndex(group.Stack, functionSuffix) >= 0 {
			return group, nil
		}
	}
	return Group{}, fmt.Errorf("no group contains function suffix %q: %#v", functionSuffix, analysis.Groups)
}

func realFrameIndex(stack []profile.Frame, functionSuffix string) int {
	for index, frame := range stack {
		if strings.HasSuffix(frame.Function, functionSuffix) || frame.Function == functionSuffix {
			return index
		}
	}
	return -1
}

func realLabelHasValue(labels []LabelKeySummary, key, value string, count int64) bool {
	for _, label := range labels {
		if label.Key != key || label.Present != count || label.Missing != 0 {
			continue
		}
		for _, candidate := range label.Values {
			if candidate.Value == value && candidate.Count == count {
				return true
			}
		}
	}
	return false
}

func formatRealStack(stack []profile.Frame) string {
	formatted := make([]string, len(stack))
	for index, frame := range stack {
		formatted[index] = fmt.Sprintf("%s(inlined=%t)", frame.Function, frame.Inlined)
	}
	return strings.Join(formatted, " -> ")
}

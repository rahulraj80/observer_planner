package arena

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// TestFakeBinSurvivesConcurrentExec pins T-404: the parallel Arena tests each
// write a fake harness script and exec it at once. A fork in one goroutine can
// inherit another goroutine's still-open write descriptor for a script being
// written, and an exec of that script then fails with "text file busy"
// (ETXTBSY) until the forked child reaches its own exec. writeFakeBin must
// make the write invisible to concurrent forks so every exec succeeds.
func TestFakeBinSurvivesConcurrentExec(t *testing.T) {
	t.Parallel()
	const workers, rounds = 16, 40
	root := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, workers*rounds)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				path := filepath.Join(root, fmt.Sprintf("fake-%d-%d", w, i))
				if err := writeFakeBin(path, "exit 0"); err != nil {
					errs <- err
					return
				}
				if err := exec.Command(path).Run(); err != nil {
					errs <- fmt.Errorf("exec %s: %w", filepath.Base(path), err)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

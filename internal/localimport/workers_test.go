package localimport

import "testing"

func TestImportWorkerCount(t *testing.T) {
	for _, tc := range []struct{ cpus, want int }{{0, 1}, {1, 1}, {2, 1}, {3, 1}, {4, 2}, {8, 4}, {16, 8}, {32, 16}, {64, 32}, {128, 64}} {
		if got := importWorkerCount(tc.cpus); got != tc.want {
			t.Errorf("%d CPUs: got %d workers, want %d", tc.cpus, got, tc.want)
		}
	}
}

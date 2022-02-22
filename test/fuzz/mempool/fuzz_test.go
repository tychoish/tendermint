package mempool_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	mempool "github.com/tendermint/tendermint/test/fuzz/mempool"
)

const testdataCasesDir = "testdata/cases"

func TestMempoolTestdataCases(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entries, err := os.ReadDir(testdataCasesDir)
	require.NoError(t, err)

	for _, e := range entries {
		entry := e
		t.Run(entry.Name(), func(t *testing.T) {
			defer func() {
				r := recover()
				require.Nilf(t, r, "testdata/cases test panic")
			}()
			f, err := os.Open(filepath.Join(testdataCasesDir, entry.Name()))
			require.NoError(t, err)
			input, err := io.ReadAll(f)
			require.NoError(t, err)
			require.NoError(t, mempool.Fuzz(ctx, input))
		})
	}
}

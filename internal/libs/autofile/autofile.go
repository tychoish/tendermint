package autofile

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	tmrand "github.com/tendermint/tendermint/libs/rand"
)

/* AutoFile usage

// Create/Append to ./autofile_test
af, err := OpenAutoFile("autofile_test")
if err != nil {
	panic(err)
}

// Stream of writes.
// During this time, the file may be moved e.g. by logRotate.
for i := 0; i < 60; i++ {
	af.Write([]byte(Fmt("LOOP(%v)", i)))
	time.Sleep(time.Second)
}

// Close the AutoFile
err = af.Close()
if err != nil {
	panic(err)
}
*/

const (
	autoFileClosePeriod = 1000 * time.Millisecond
	autoFilePerms       = os.FileMode(0600)
)

// AutoFile automatically closes and re-opens file for writing. The file is
// automatically setup to close itself every 1s and upon receiving SIGHUP.
//
// This is useful for using a log file with the logrotate tool.
type AutoFile struct {
	ID   string
	Path string

	closeTicker      *time.Ticker
	closeTickerStopc chan struct{} // closed when closeTicker is stopped
	hupc             chan os.Signal

	mtx      sync.Mutex
	isClosed bool
	file     *os.File
}

// OpenAutoFile creates an AutoFile in the path (with random ID). If there is
// an error, it will be of type *PathError or *ErrPermissionsChanged (if file's
// permissions got changed (should be 0600)).
func OpenAutoFile(ctx context.Context, path string) (*AutoFile, error) {
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	af := &AutoFile{
		ID:               tmrand.Str(12) + ":" + path,
		Path:             path,
		closeTicker:      time.NewTicker(autoFileClosePeriod),
		closeTickerStopc: make(chan struct{}),
	}
	if err := af.openFile(); err != nil {
		af.Close()
		return nil, err
	}

	// Close file on SIGHUP.
	af.hupc = make(chan os.Signal, 1)
	signal.Notify(af.hupc, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-af.hupc:
				_ = af.closeFile(false)
			case <-ctx.Done():
				return
			}
		}
	}()

	go af.closeFileRoutine(ctx)

	return af, nil
}

// Close shuts down the closing goroutine, SIGHUP handler and closes the
// AutoFile.
func (af *AutoFile) Close() error {
	af.closeTicker.Stop()
	close(af.closeTickerStopc)
	if af.hupc != nil {
		close(af.hupc)
	}
	return af.closeFile(true)
}

func (af *AutoFile) closeFileRoutine(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			_ = af.closeFile(true)
			return
		case <-af.closeTicker.C:
			_ = af.closeFile(false)
		case <-af.closeTickerStopc:
			return
		}
	}
}

func (af *AutoFile) closeFile(forGood bool) (err error) {
	af.mtx.Lock()
	defer af.mtx.Unlock()
	defer func() {
		if forGood {
			af.isClosed = true
		}
	}()
	if af.isClosed {
		return os.ErrClosed
	}

	file := af.file
	if file == nil {
		return nil
	}

	af.file = nil
	err = file.Close()

	return
}

// Write writes len(b) bytes to the AutoFile. It returns the number of bytes
// written and an error, if any. Write returns a non-nil error when n !=
// len(b).
// Opens AutoFile if needed.
func (af *AutoFile) Write(b []byte) (n int, err error) {
	af.mtx.Lock()
	defer af.mtx.Unlock()

	if af.file == nil {
		if err = af.openFile(); err != nil {
			return
		}
	}

	n, err = af.file.Write(b)
	return
}

// Sync commits the current contents of the file to stable storage. Typically,
// this means flushing the file system's in-memory copy of recently written
// data to disk.
// Opens AutoFile if needed.
func (af *AutoFile) Sync() error {
	af.mtx.Lock()
	defer af.mtx.Unlock()

	if af.file == nil {
		if err := af.openFile(); err != nil {
			return err
		}
	}
	return af.file.Sync()
}

func (af *AutoFile) openFile() error {
	if af.isClosed {
		return os.ErrClosed
	}

	file, err := os.OpenFile(af.Path, os.O_RDWR|os.O_CREATE|os.O_APPEND, autoFilePerms)
	if err != nil {
		return err
	}
	// fileInfo, err := file.Stat()
	// if err != nil {
	// 	return err
	// }
	// if fileInfo.Mode() != autoFilePerms {
	// 	return errors.NewErrPermissionsChanged(file.Name(), fileInfo.Mode(), autoFilePerms)
	// }
	af.file = file
	return nil
}

// Size returns the size of the AutoFile. It returns -1 and an error if fails
// get stats or open file.
// Opens AutoFile if needed.
func (af *AutoFile) Size() (int64, error) {
	af.mtx.Lock()
	defer af.mtx.Unlock()

	if af.file == nil {
		if err := af.openFile(); err != nil {
			return -1, err
		}
	}

	if af.isClosed {
		return -1, os.ErrClosed
	}

	stat, err := af.file.Stat()
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}

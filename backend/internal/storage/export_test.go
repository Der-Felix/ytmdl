package storage

import "os"

// SetChmodFuncForTest allows unit tests to inject custom chmod behavior and
// returns a function to restore the original.
func SetChmodFuncForTest(fn func(name string, mode os.FileMode) error) func() {
	original := chmodFunc
	chmodFunc = fn
	return func() {
		chmodFunc = original
	}
}

// SetQueryFSFuncForTest allows unit tests to inject custom filesystem query behavior
// and returns a function to restore the original.
func SetQueryFSFuncForTest(fn func(path string) (fsType string, total, free, avail uint64, err error)) func() {
	original := queryFSFunc
	queryFSFunc = fn
	return func() {
		queryFSFunc = original
	}
}

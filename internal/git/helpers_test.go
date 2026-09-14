package git_test

import "os"

func removeAll(p string) error { return os.RemoveAll(p) }

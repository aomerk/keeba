package cli

// silentExit is returned by commands that have already printed a summary
// and just want a non-zero exit code without main() echoing the error
// string.
type silentExit struct{}

func (silentExit) Error() string { return "silent exit" }

// IsSilentExit reports whether err is the silent-exit sentinel.
func IsSilentExit(err error) bool {
	_, ok := err.(silentExit)
	return ok
}

// errSilentFail is the shared instance returned by lint/drift/meta when
// they already printed their report.
var errSilentFail silentExit

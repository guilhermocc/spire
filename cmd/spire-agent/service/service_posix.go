//go:build !windows
// +build !windows

package service

func (r Runner) Run() int {
	return 0
}

func (r Runner) NeedToRunAsAService() bool {
	return false
}

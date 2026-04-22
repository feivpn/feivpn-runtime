package platform

import "fmt"

// stub is shared by linux_stub.go and darwin_stub.go. Methods always
// return UNSUPPORTED_PLATFORM.
type stub struct{ name string }

func (s *stub) Name() string                           { return s.name }
func (s *stub) InstallService(_ InstallOptions) error  { return errStub() }
func (s *stub) EnableAndStart() error                  { return errStub() }
func (s *stub) Stop() error                            { return errStub() }
func (s *stub) Disable() error                         { return errStub() }
func (s *stub) Uninstall() error                       { return errStub() }
func (s *stub) IsActive() (bool, error)                { return false, errStub() }

func errStub() error {
	return fmt.Errorf("UNSUPPORTED_PLATFORM: this build is a stub; recompile on linux or darwin")
}

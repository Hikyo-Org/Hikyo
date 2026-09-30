package badseam

type unrelated struct{ ReauthWindow int }

func (*unrelated) BumpSchemaRevision(int)     {}
func (*unrelated) effectiveReauthWindow() int { return 0 }

func decoy(s *unrelated) int {
	s.BumpSchemaRevision(1)
	return s.ReauthWindow
}

func (s *ProjectSettings) SetEnvironment() int { return s.Auth.ReauthWindow }

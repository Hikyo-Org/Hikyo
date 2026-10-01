package store

type adapterConfigConfiguredNamesRow struct {
	TargetID      string
	Prefix        string
	CanonicalName string
}
type adapterConfigPendingNamesRow struct {
	TargetID      string
	EffectiveName string
}
type adapterConfigAWSConfiguredNamesRow struct {
	TargetID string
	Kind     string
	Name     string
	Prefix   string
	KeyName  string
}
type adapterConfigAWSPendingNamesRow struct {
	TargetID      string
	EffectiveName string
}

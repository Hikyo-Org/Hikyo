package store

type adapterMoveGetRow struct {
	ID                   string
	AdapterID            string
	Kind                 string
	State                string
	Keep                 bool
	PendingOrigin        string
	Created              adapterStoredTime
	AuthorityPrincipalID string
}

type adapterMoveTargetsRow struct {
	TargetID               string
	EnvironmentID          string
	DestinationKind        string
	DestinationOwner       string
	DestinationName        string
	DestinationEnvironment string
	DestinationScope       string
	DestinationID          int64
	RepositoryID           int64
	Visibility             string
	SelectedJSON           []byte
	NamePrefix             string
	OrphanJSON             []byte
}

type adapterMoveJobsRow struct {
	ID       string
	TargetID string
	Kind     string
	State    string
}

type adapterMoveCancelTargetRow struct {
	Generation   int64
	ProviderBusy int
}

type adapterMoveBeginOriginAdapterRow struct {
	CurrentOrigin string
	ProviderBusy  int
}

type adapterMoveBeginOriginTargetsRow struct {
	Id                     string
	EnvironmentID          string
	Kind                   string
	Owner                  string
	Name                   string
	DestinationEnvironment string
	DestinationScope       string
	DestinationID          int64
	RepositoryID           int64
	Visibility             string
	SelectedRaw            []byte
	Prefix                 string
	Generation             int64
	ActiveJob              string
	OrphanRaw              []byte
}

type adapterMoveBeginTargetRow struct {
	AdapterID              string
	Origin                 string
	EnvironmentID          string
	Kind                   string
	Owner                  string
	Name                   string
	DestinationEnvironment string
	DestinationScope       string
	DestinationID          int64
	Prefix                 string
	Generation             int64
	ActiveJob              string
	ProviderBusy           int
	OrphanRaw              []byte
}

type adapterMoveAWSConfiguredNamesRow struct {
	TargetID string
	Kind     string
	Name     string
	Prefix   string
	KeyName  string
}

type adapterMoveAWSPendingNamesRow struct {
	OtherTarget string
	Effective   string
}

type adapterMoveFlagsRow struct {
	Provider  string
	Protected bool
	Hidden    bool
	Expand    bool
}

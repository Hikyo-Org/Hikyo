package owner

type Resolver struct{}

func (Resolver) SetQueryObserver() {}
func decoy(r Resolver)             { r.SetQueryObserver() }

package badseam

type Auth struct{ ReauthWindow int }
type CatalogueRepo interface{ BumpSchemaRevision() }
type ProjectSettings struct{ Auth *Auth }

func (a *Auth) effectiveReauthWindow() int { return a.ReauthWindow }
func bumpSchemaRevision(c CatalogueRepo)   { c.BumpSchemaRevision() }

// Renaming the receiver must not bypass the duration seam.
func bypassWindow(auth *Auth) int { return auth.ReauthWindow } // want "ReauthWindow access outside effectiveReauthWindow"

// Capturing a method value must not bypass the budget seam.
func bypassBudget(c CatalogueRepo) func() { return c.BumpSchemaRevision } // want "BumpSchemaRevision access outside bumpSchemaRevision"

func (s *ProjectSettings) otherSettingsMethod() int { return s.Auth.ReauthWindow } // want "ReauthWindow access outside effectiveReauthWindow"

type localCatalogue interface{ BumpSchemaRevision() }

func bypassLocalInterface(c localCatalogue) { c.BumpSchemaRevision() }                  // want "BumpSchemaRevision access outside bumpSchemaRevision"
func bypassTypeAssertion(c CatalogueRepo)   { c.(localCatalogue).BumpSchemaRevision() } // want "BumpSchemaRevision access outside bumpSchemaRevision"

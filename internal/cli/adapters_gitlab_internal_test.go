package cli

import "testing"

func TestGitLabTargetInputMapsKindsAndFlags(t *testing.T) {
	flags := gitLabTargetFlags{scope: "production", protected: true, hidden: true}
	input, err := adapterTargetInput("env_1", "project", "platform/backend", "api", "", "", "", "", "key_1", adapterKeySelection{}, &flags)
	if err != nil {
		t.Fatal(err)
	}
	if input.DestinationKind != "repository" || input.DestinationOwner != "platform/backend" || input.DestinationName != "api" {
		t.Fatalf("project mapping = %+v", input)
	}
	if *input.DestinationScope != "production" || !*input.VariableProtected || !*input.VariableHidden || *input.VariableExpand {
		t.Fatalf("flags = %+v", input)
	}
	group, err := adapterTargetInput("env_1", "group", "platform", "", "", "", "", "", "key_1", adapterKeySelection{}, &gitLabTargetFlags{})
	if err != nil || group.DestinationKind != "organization" || group.Visibility != "" {
		t.Fatalf("group mapping = %+v, %v", group, err)
	}
}

func TestGitLabTargetInputRefusesGitHubRouting(t *testing.T) {
	for name, args := range map[string][5]string{
		"group with repo":         {"group", "platform", "api", "", ""},
		"environment kind":        {"environment", "platform", "api", "prod", ""},
		"visibility":              {"group", "platform", "", "", "all"},
		"project without repo":    {"project", "platform", "", "", ""},
		"destination environment": {"project", "platform", "api", "prod", ""},
	} {
		if _, err := adapterTargetInput("env_1", args[0], args[1], args[2], args[3], args[4], "", "", "key_1", adapterKeySelection{}, &gitLabTargetFlags{}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGitLabModeDetection(t *testing.T) {
	if !gitLabMode("gitlab", "repository", gitLabTargetFlags{}) || !gitLabMode("", "group", gitLabTargetFlags{}) || !gitLabMode("", "repository", gitLabTargetFlags{scope: "*"}) {
		t.Fatal("GitLab signals not detected")
	}
	if gitLabMode("github-actions", "organization", gitLabTargetFlags{}) {
		t.Fatal("GitHub target misdetected as GitLab")
	}
}

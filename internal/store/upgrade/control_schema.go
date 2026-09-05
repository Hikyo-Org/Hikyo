package upgrade

import (
	"encoding/json"
	"reflect"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func splitControl(c Catalog) (Catalog, []string, error) {
	remaining := c
	remaining.Objects = []string{}
	control := []string{}
	for _, object := range c.Objects {
		var fields []json.RawMessage
		if err := json.Unmarshal([]byte(object), &fields); err != nil {
			return Catalog{}, nil, ErrCorrupt
		}
		index := 2
		if len(fields) <= index {
			remaining.Objects = append(remaining.Objects, object)
			continue
		}
		var owner string
		if err := json.Unmarshal(fields[index], &owner); err != nil {
			return Catalog{}, nil, ErrCorrupt
		}
		owned := owner == "upgrade_control" || owner == "upgrade_pending" || owner == "upgrade_nonces"
		if c.Engine == releaseidentity.Postgres {
			owned = owned || owner == "upgrade_control_pkey" || owner == "upgrade_pending_pkey" || owner == "upgrade_nonces_pkey"
		}
		if owned {
			control = append(control, object)
		} else {
			remaining.Objects = append(remaining.Objects, object)
		}
	}
	return remaining, control, nil
}

func withoutControl(c Catalog) (Catalog, error) {
	remaining, control, err := splitControl(c)
	if err != nil {
		return Catalog{}, err
	}
	raw, err := genesisFS.ReadFile("genesis/control-" + string(c.Engine) + ".json")
	if err != nil {
		return Catalog{}, err
	}
	var expected []string
	if err := json.Unmarshal(raw, &expected); err != nil {
		return Catalog{}, err
	}
	if !reflect.DeepEqual(control, expected) {
		return Catalog{}, ErrCorrupt
	}
	return remaining, nil
}

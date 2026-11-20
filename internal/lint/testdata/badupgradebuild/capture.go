package badupgradebuild

import disguised "github.com/Hikyo-Org/hikyo/internal/store/upgrade"

var RuntimeMigration = disguised.BuildScratchSchema // want "scratch schema generation is confined to app/releasecompat\\.go"

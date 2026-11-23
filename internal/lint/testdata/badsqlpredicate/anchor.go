package badsqlpredicate // want "sqlpredicate\\(sqlite\\): table \"unclassified\" has no hikyo:table scope directive" "sqlpredicate\\(postgres\\): table \"unclassified\" has no hikyo:table scope directive" "sqlpredicate\\(sqlite\\): GetWidgetUnscoped: missing top-level chain conjunct on \"org_id\"" "sqlpredicate\\(sqlite\\): GetWidgetUnscoped: missing top-level chain conjunct on \"project_id\"" "sqlpredicate\\(postgres\\): GetWidgetUnscoped: missing top-level chain conjunct on \"org_id\"" "sqlpredicate\\(postgres\\): GetWidgetUnscoped: missing top-level chain conjunct on \"project_id\"" "sqlpredicate: query \"GetWidgetScoped\" has no generated API contract" "sqlpredicate: query \"GetWidgetUnscoped\" has no generated API contract"

// The anchor the sqlpredicate analyzer runs on; the migrations, queries and
// (empty) generated packages sit under the fixture root. -nopins holds the fixture to no
// reviewed pins, so the real repository's pins do not read as stale here.

package store

import (
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func (d sqliteAdoptDB) sshQueries() sshRuntimeQueries {
	return sqliteSshRuntimeQueries{queries: sqlitegen.New(d.db)}
}

func (d sqliteAdoptDB) dynamicQueries() dynamicRuntimeQueries {
	return sqliteDynamicRuntimeQueries{queries: sqlitegen.New(d.db)}
}

func (d pgAdoptDB) sshQueries() sshRuntimeQueries {
	return pgSshRuntimeQueries{queries: pggen.New(d.db)}
}

func (d pgAdoptDB) dynamicQueries() dynamicRuntimeQueries {
	return pgDynamicRuntimeQueries{queries: pggen.New(d.db)}
}

func (d sqliteAdapterTx) sshQueries() sshRuntimeQueries {
	return sqliteSshRuntimeQueries{queries: sqlitegen.New(d.tx)}
}

func (d sqliteAdapterTx) dynamicQueries() dynamicRuntimeQueries {
	return sqliteDynamicRuntimeQueries{queries: sqlitegen.New(d.tx)}
}

func (d pgAdapterTx) sshQueries() sshRuntimeQueries {
	return pgSshRuntimeQueries{queries: pggen.New(d.tx)}
}

func (d pgAdapterTx) dynamicQueries() dynamicRuntimeQueries {
	return pgDynamicRuntimeQueries{queries: pggen.New(d.tx)}
}

func (d sqliteAdoptDB) adapterStoreQueries() adapterStoreQueries {
	return sqliteAdapterStoreQueries{queries: sqlitegen.New(d.db)}
}

func (d pgAdoptDB) adapterStoreQueries() adapterStoreQueries {
	return pgAdapterStoreQueries{queries: pggen.New(d.db)}
}

func (d sqliteAdapterTx) adapterStoreQueries() adapterStoreQueries {
	return sqliteAdapterStoreQueries{queries: sqlitegen.New(d.tx)}
}

func (d pgAdapterTx) adapterStoreQueries() adapterStoreQueries {
	return pgAdapterStoreQueries{queries: pggen.New(d.tx)}
}

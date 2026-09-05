package upgrade

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Catalog is a deterministic structural inventory. Runtime rows, owner names,
// grants, sequence current values and physical OIDs are intentionally excluded.
// The schema declaration owns objects, definitions and sequence configuration;
// operational grants/ownership remain installation custody, not schema identity.
type Catalog struct {
	Format  string                 `json:"format"`
	Engine  releaseidentity.Engine `json:"engine"`
	Objects []string               `json:"objects"`
	Applied []int64                `json:"applied"`
}

func (c Catalog) Digest() releaseidentity.Digest {
	raw, _ := json.Marshal(c)
	return releaseidentity.Hash(raw)
}

type catalogRows interface {
	migrationRows
	Close()
}
type sqlCatalogRows struct{ *sql.Rows }

func (r sqlCatalogRows) Close() { _ = r.Rows.Close() }

type catalogQuery func(context.Context, string, ...any) (catalogRows, error)

func inspectCatalog(ctx context.Context, q queryer, engine releaseidentity.Engine) (Catalog, error) {
	return inspectCatalogWith(ctx, func(ctx context.Context, query string, args ...any) (catalogRows, error) {
		r, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return sqlCatalogRows{r}, nil
	}, engine)
}
func inspectCatalogObjectsWith(ctx context.Context, queryRows catalogQuery, engine releaseidentity.Engine) (Catalog, error) {
	c := Catalog{Format: "hikyo-schema/v1", Engine: engine, Objects: []string{}, Applied: []int64{}}
	query := sqliteCatalogSQL
	if engine == releaseidentity.Postgres {
		query = postgresCatalogSQL
	}
	rows, err := queryRows(ctx, query)
	if err != nil {
		return Catalog{}, fmt.Errorf("upgrade: inspect schema: %w", err)
	}
	for rows.Next() {
		var object string
		if err := rows.Scan(&object); err != nil {
			rows.Close()
			return Catalog{}, err
		}
		if len(c.Objects) >= 32768 || len(object) > 1<<20 {
			rows.Close()
			return Catalog{}, ErrGenesis
		}
		c.Objects = append(c.Objects, object)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Catalog{}, err
	}
	slices.Sort(c.Objects)
	return c, nil
}

func inspectCatalogWith(ctx context.Context, queryRows catalogQuery, engine releaseidentity.Engine) (Catalog, error) {
	c, err := inspectCatalogObjectsWith(ctx, queryRows, engine)
	if err != nil {
		return Catalog{}, err
	}
	var rows catalogRows
	var hasGoose bool
	if engine == releaseidentity.SQLite {
		rows, err = queryRows(ctx, `SELECT name FROM sqlite_schema WHERE type='table' AND name='goose_db_version'`)
	} else {
		rows, err = queryRows(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname='goose_db_version' AND c.relkind='r'`)
	}
	if err != nil {
		return Catalog{}, err
	}
	hasGoose = rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Catalog{}, err
	}
	if !hasGoose {
		return c, nil
	}
	// Canonical legacy goose history is one applied row per version, including
	// exactly one zero bookkeeping row. Down/duplicate/unknown history refuses.
	rows, err = queryRows(ctx, `SELECT version_id,is_applied FROM goose_db_version ORDER BY id`)
	if err != nil {
		return Catalog{}, err
	}
	defer rows.Close()
	var previous int64 = -1
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil {
			return Catalog{}, err
		}
		if !applied || version <= previous || (previous == -1 && version != 0) {
			return Catalog{}, ErrGenesis
		}
		previous = version
		c.Applied = append(c.Applied, version)
	}
	if err := rows.Err(); err != nil {
		return Catalog{}, err
	}
	if len(c.Applied) == 0 {
		return Catalog{}, ErrGenesis
	}
	return c, nil
}

const sqliteCatalogSQL = `SELECT json_array(type,name,tbl_name,sql) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`

// PostgreSQL catalog output excludes OIDs and schema owners so independently
// created databases with the same migration bytes compare exactly. Include
// every non-system namespace; an added schema/function/policy is drift too.
const postgresCatalogSQL = `WITH ns AS (
 SELECT oid,nspname FROM pg_namespace WHERE nspname !~ '^pg_' AND nspname <> 'information_schema'
), objects AS (
 SELECT json_build_array('schema',nspname)::text AS object FROM ns
 UNION ALL SELECT json_build_array('relation',n.nspname,c.relname,c.relkind,c.relpersistence,c.relrowsecurity,c.relforcerowsecurity,c.relreplident,c.reloptions)::text FROM pg_class c JOIN ns n ON n.oid=c.relnamespace
 UNION ALL SELECT json_build_array('column',n.nspname,c.relname,a.attnum,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,a.attidentity,a.attgenerated,pg_get_expr(d.adbin,d.adrelid),co.collname)::text FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN ns n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum LEFT JOIN pg_collation co ON co.oid=a.attcollation WHERE a.attnum>0 AND NOT a.attisdropped
 UNION ALL SELECT json_build_array('constraint',n.nspname,c.relname,k.conname,k.contype,k.convalidated,pg_get_constraintdef(k.oid,false))::text FROM pg_constraint k JOIN ns n ON n.oid=k.connamespace LEFT JOIN pg_class c ON c.oid=k.conrelid
 UNION ALL SELECT json_build_array('index',n.nspname,c.relname,pg_get_indexdef(i.indexrelid),i.indisvalid,i.indisready,i.indisreplident)::text FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN ns n ON n.oid=c.relnamespace
 UNION ALL SELECT json_build_array('sequence',n.nspname,c.relname,format_type(s.seqtypid,-1),s.seqstart,s.seqincrement,s.seqmax,s.seqmin,s.seqcache,s.seqcycle)::text FROM pg_sequence s JOIN pg_class c ON c.oid=s.seqrelid JOIN ns n ON n.oid=c.relnamespace
 UNION ALL SELECT json_build_array('view',n.nspname,c.relname,pg_get_viewdef(c.oid,false))::text FROM pg_class c JOIN ns n ON n.oid=c.relnamespace WHERE c.relkind IN ('v','m')
 UNION ALL SELECT json_build_array('trigger',n.nspname,c.relname,t.tgname,t.tgenabled,pg_get_triggerdef(t.oid,false))::text FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN ns n ON n.oid=c.relnamespace WHERE NOT t.tgisinternal
 UNION ALL SELECT json_build_array('rule',n.nspname,c.relname,r.rulename,r.ev_enabled,pg_get_ruledef(r.oid,false))::text FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class JOIN ns n ON n.oid=c.relnamespace
 UNION ALL SELECT json_build_array('function',n.nspname,p.proname,pg_get_function_identity_arguments(p.oid),p.prosrc,p.probin,p.prokind,p.provolatile,p.proisstrict,p.prosecdef,p.proleakproof,p.proparallel,p.proconfig,format_type(p.prorettype,-1),l.lanname)::text FROM pg_proc p JOIN ns n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang
 UNION ALL SELECT json_build_array('type',n.nspname,t.typname,t.typtype,format_type(t.typbasetype,t.typtypmod),t.typnotnull,t.typdefault)::text FROM pg_type t JOIN ns n ON n.oid=t.typnamespace WHERE t.typtype <> 'c' AND t.typelem=0
 UNION ALL SELECT json_build_array('enum',n.nspname,t.typname,e.enumsortorder,e.enumlabel)::text FROM pg_enum e JOIN pg_type t ON t.oid=e.enumtypid JOIN ns n ON n.oid=t.typnamespace
 UNION ALL SELECT json_build_array('policy',schemaname,tablename,policyname,permissive,roles,cmd,qual,with_check)::text FROM pg_policies WHERE schemaname IN (SELECT nspname FROM ns)
 UNION ALL SELECT json_build_array('extension',extname,extversion)::text FROM pg_extension WHERE extname <> 'plpgsql'
 UNION ALL SELECT json_build_array('operator',n.nspname,o.oprname,o.oprleft::regtype::text,o.oprright::regtype::text)::text FROM pg_operator o JOIN ns n ON n.oid=o.oprnamespace
 UNION ALL SELECT json_build_array('operator-class',n.nspname,o.opcname)::text FROM pg_opclass o JOIN ns n ON n.oid=o.opcnamespace
 UNION ALL SELECT json_build_array('operator-family',n.nspname,o.opfname)::text FROM pg_opfamily o JOIN ns n ON n.oid=o.opfnamespace
 UNION ALL SELECT json_build_array('conversion',n.nspname,c.conname)::text FROM pg_conversion c JOIN ns n ON n.oid=c.connamespace
 UNION ALL SELECT json_build_array('statistics',n.nspname,s.stxname)::text FROM pg_statistic_ext s JOIN ns n ON n.oid=s.stxnamespace
 UNION ALL SELECT json_build_array('text-search-config',n.nspname,c.cfgname)::text FROM pg_ts_config c JOIN ns n ON n.oid=c.cfgnamespace
 UNION ALL SELECT json_build_array('text-search-dictionary',n.nspname,d.dictname)::text FROM pg_ts_dict d JOIN ns n ON n.oid=d.dictnamespace
 UNION ALL SELECT json_build_array('text-search-parser',n.nspname,p.prsname)::text FROM pg_ts_parser p JOIN ns n ON n.oid=p.prsnamespace
 UNION ALL SELECT json_build_array('text-search-template',n.nspname,t.tmplname)::text FROM pg_ts_template t JOIN ns n ON n.oid=t.tmplnamespace
 UNION ALL SELECT json_build_array('event-trigger',evtname)::text FROM pg_event_trigger
 UNION ALL SELECT json_build_array('foreign-server',srvname)::text FROM pg_foreign_server
 UNION ALL SELECT json_build_array('foreign-wrapper',fdwname)::text FROM pg_foreign_data_wrapper
 UNION ALL SELECT json_build_array('publication',pubname)::text FROM pg_publication
 UNION ALL SELECT json_build_array('subscription',subname)::text FROM pg_subscription WHERE subdbid=(SELECT oid FROM pg_database WHERE datname=current_database())
 UNION ALL SELECT json_build_array('cast',castsource::regtype::text,casttarget::regtype::text,castcontext,castmethod)::text FROM pg_cast WHERE oid>=16384
 UNION ALL SELECT json_build_array('transform',t.trftype::regtype::text,l.lanname)::text FROM pg_transform t JOIN pg_language l ON l.oid=t.trflang
 UNION ALL SELECT json_build_array('unsupported-large-object')::text FROM pg_largeobject_metadata
 UNION ALL SELECT json_build_array('collation',n.nspname,c.collname,c.collprovider,c.collisdeterministic,c.collencoding,c.collcollate,c.collctype,COALESCE(to_jsonb(c)->>'colllocale',to_jsonb(c)->>'colliculocale'),c.collversion)::text FROM pg_collation c JOIN ns n ON n.oid=c.collnamespace
) SELECT object FROM objects ORDER BY object`

func controlPresent(c Catalog) bool {
	for _, object := range c.Objects {
		if strings.Contains(object, `"upgrade_control"`) || strings.Contains(object, `"upgrade_pending"`) || strings.Contains(object, `"upgrade_nonces"`) {
			return true
		}
	}
	return false
}

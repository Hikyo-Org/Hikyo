package badtxresult

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func repositories(ctx context.Context, db *store.DB) {
	_, _ = tx.WriteResult(ctx, db, func(context.Context, store.Repos, *authz.TxAuthorizer) (store.Repos, error) { // want "tx\\.WriteResult result .*internal/store\\.Repos is not detached data: .*internal/store\\.Repos"
		return nil, nil
	})
}

func hiddenInterface(ctx context.Context, db *store.DB) {
	type result struct{ Value any }
	_, _ = tx.WriteResult(ctx, db, func(context.Context, store.Repos, *authz.TxAuthorizer) (result, error) { // want "tx\\.WriteResult result .*internal/lint/testdata/badtxresult\\.result is not detached data: interface values can retain attempt-owned authority"
		return result{}, nil
	})
}

func closure(ctx context.Context, db *store.DB) {
	_, _ = tx.WriteResult(ctx, db, func(context.Context, store.Repos, *authz.TxAuthorizer) (func(), error) { // want "tx\\.WriteResult result func\\(\\) is not detached data: function values can capture attempt-owned authority"
		return func() {}, nil
	})
}

package store

import (
	"math"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func TestTransitKeyReadsRefuseCorruptVersionBounds(t *testing.T) {
	fields := []string{"latest_version", "min_encrypt_version", "min_decrypt_version", "min_available_version", "compromised_through_version"}
	for index, field := range fields {
		for _, value := range []int64{-1, math.MaxUint32 + 1} {
			versions := [5]int64{1, 1, 1, 1, 0}
			versions[index] = value
			key := sqlitegen.TransitKey{ID: "key", LatestVersion: versions[0], MinEncryptVersion: versions[1], MinDecryptVersion: versions[2], MinAvailableVersion: versions[3], CompromisedThroughVersion: versions[4]}
			for _, read := range []func() error{
				func() error { _, err := sqliteTransitKey(key); return err },
				func() error {
					_, err := sqliteTransitDeletion(sqlitegen.TransitSelectDeletionDueRow{TransitKey: key})
					return err
				},
				func() error {
					_, err := sqliteTransitRotation(sqlitegen.TransitSelectRotationDueRow{TransitKey: key})
					return err
				},
			} {
				if err := read(); err == nil || !strings.Contains(err.Error(), field) {
					t.Fatalf("SQLite %s=%d admitted: %v", field, value, err)
				}
			}
		}
		// PostgreSQL INTEGER cannot contain a uint32 overflow, but can contain negatives.
		versions := [5]int32{1, 1, 1, 1, 0}
		versions[index] = -1
		key := pggen.TransitKey{ID: "key", LatestVersion: versions[0], MinEncryptVersion: versions[1], MinDecryptVersion: versions[2], MinAvailableVersion: versions[3], CompromisedThroughVersion: versions[4]}
		for _, read := range []func() error{
			func() error { _, err := pgTransitKey(key); return err },
			func() error {
				_, err := pgTransitDeletion(pggen.TransitSelectDeletionDueRow{TransitKey: key})
				return err
			},
			func() error {
				_, err := pgTransitRotation(pggen.TransitSelectRotationDueRow{TransitKey: key})
				return err
			},
		} {
			if err := read(); err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("PostgreSQL %s=-1 admitted: %v", field, err)
			}
		}
	}
}

func TestTransitMaterialReadsRefuseWrapping(t *testing.T) {
	for _, value := range []int64{-1, math.MaxUint32 + 1} {
		for _, read := range []func() error{
			func() error {
				_, err := sqliteTransitVersion(sqlitegen.TransitListVersionsRow{Version: value})
				return err
			},
			func() error {
				_, err := sqliteTransitMaterial(sqlitegen.TransitVersionMaterialRow{Version: value})
				return err
			},
			func() error {
				_, err := sqliteTransitExternal(sqlitegen.TransitExternalVersionsRow{Version: value})
				return err
			},
		} {
			if err := read(); err == nil {
				t.Fatalf("SQLite material version %d admitted", value)
			}
		}
	}
	for _, read := range []func() error{
		func() error { _, err := pgTransitVersion(pggen.TransitListVersionsRow{Version: -1}); return err },
		func() error { _, err := pgTransitMaterial(pggen.TransitVersionMaterialRow{Version: -1}); return err },
		func() error { _, err := pgTransitExternal(pggen.TransitExternalVersionsRow{Version: -1}); return err },
	} {
		if err := read(); err == nil {
			t.Fatal("PostgreSQL negative material version admitted")
		}
	}
	key, err := sqliteTransitKey(sqlitegen.TransitKey{LatestVersion: math.MaxUint32, MinEncryptVersion: 0, MinDecryptVersion: 1, MinAvailableVersion: 1, CompromisedThroughVersion: 0})
	if err != nil || key.LatestVersion != math.MaxUint32 || key.MinEncryptVersion != 0 {
		t.Fatalf("valid uint32 limits refused: %+v, %v", key, err)
	}
}

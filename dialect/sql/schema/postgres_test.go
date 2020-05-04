// Copyright 2019-present Facebook Inc. All rights reserved.
// This source code is licensed under the Apache 2.0 license found
// in the LICENSE file in the root directory of this source tree.

package schema

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/facebookincubator/ent/dialect"
	"github.com/facebookincubator/ent/dialect/sql"
	"github.com/facebookincubator/ent/schema/field"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestPostgres_Create(t *testing.T) {
	tests := []struct {
		name    string
		tables  []*Table
		options []MigrateOption
		before  func(pgMock)
		wantErr bool
	}{
		{
			name: "tx failed",
			before: func(mock pgMock) {
				mock.ExpectBegin().WillReturnError(sqlmock.ErrCancelled)
			},
			wantErr: true,
		},
		{
			name: "unsupported version",
			before: func(mock pgMock) {
				mock.start("90000")
			},
			wantErr: true,
		},
		{
			name: "no tables",
			before: func(mock pgMock) {
				mock.start("120000")
				mock.ExpectCommit()
			},
		},
		{
			name: "create new table",
			tables: []*Table{
				{
					Name: "users",
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "age", Type: field.TypeInt},
						{Name: "doc", Type: field.TypeJSON, Nullable: true},
						{Name: "enums", Type: field.TypeEnum, Enums: []string{"a", "b"}},
						{Name: "uuid", Type: field.TypeUUID},
						{Name: "price", Type: field.TypeFloat64, SchemaType: map[string]string{dialect.Postgres: "numeric(5,2)"}},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "users"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, "name" varchar NULL, "age" bigint NOT NULL, "doc" jsonb NULL, "enums" varchar NOT NULL, "uuid" uuid NOT NULL, "price" numeric(5,2) NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "create new table with foreign key",
			tables: func() []*Table {
				var (
					c1 = []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "created_at", Type: field.TypeTime},
					}
					c2 = []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString},
						{Name: "owner_id", Type: field.TypeInt, Nullable: true},
					}
					t1 = &Table{
						Name:       "users",
						Columns:    c1,
						PrimaryKey: c1[0:1],
					}
					t2 = &Table{
						Name:       "pets",
						Columns:    c2,
						PrimaryKey: c2[0:1],
						ForeignKeys: []*ForeignKey{
							{
								Symbol:     "pets_owner",
								Columns:    c2[2:],
								RefTable:   t1,
								RefColumns: c1[0:1],
								OnDelete:   Cascade,
							},
						},
					}
				)
				return []*Table{t1, t2}
			}(),
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "users"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, "name" varchar NULL, "created_at" timestamp with time zone NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.tableExists("pets", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "pets"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, "name" varchar NOT NULL, "owner_id" bigint NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.fkExists("pets_owner", false)
				mock.ExpectExec(escape(`ALTER TABLE "pets" ADD CONSTRAINT "pets_owner" FOREIGN KEY("owner_id") REFERENCES "users"("id") ON DELETE CASCADE`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "add column to table",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "uuid", Type: field.TypeUUID, Nullable: true},
						{Name: "text", Type: field.TypeString, Nullable: true, Size: math.MaxInt32},
						{Name: "age", Type: field.TypeInt},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character varying", "YES", "NULL").
						AddRow("uuid", "uuid", "YES", "NULL").
						AddRow("text", "text", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" ADD COLUMN "age" bigint NOT NULL`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "add int column with default value to table",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "age", Type: field.TypeInt, Default: 10},
						{Name: "doc", Type: field.TypeJSON, Nullable: true},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character", "YES", "NULL").
						AddRow("doc", "jsonb", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" ADD COLUMN "age" bigint NOT NULL DEFAULT 10`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "add blob columns",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "blob", Type: field.TypeBytes, Size: 1e3},
						{Name: "longblob", Type: field.TypeBytes, Size: 1e6},
						{Name: "doc", Type: field.TypeJSON, Nullable: true},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character", "YES", "NULL").
						AddRow("doc", "jsonb", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" ADD COLUMN "blob" bytea NOT NULL, ADD COLUMN "longblob" bytea NOT NULL`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "add float column with default value to table",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "age", Type: field.TypeFloat64, Default: 10.1},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" ADD COLUMN "age" double precision NOT NULL DEFAULT 10.1`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "add bool column with default value to table",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "age", Type: field.TypeBool, Default: true},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" ADD COLUMN "age" boolean NOT NULL DEFAULT true`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "add string column with default value to table",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "nick", Type: field.TypeString, Default: "unknown"},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" ADD COLUMN "nick" varchar NOT NULL DEFAULT 'unknown'`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "drop column to table",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			options: []MigrateOption{WithDropColumn(true)},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" DROP COLUMN "name"`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "modify column to nullable",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("name", "character", "NO", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`ALTER TABLE "users" ALTER COLUMN "name" TYPE varchar, ALTER COLUMN "name" DROP NOT NULL`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "apply uniqueness on column",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "age", Type: field.TypeInt, Unique: true},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("age", "bigint", "NO", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.ExpectExec(escape(`CREATE UNIQUE INDEX "users_age" ON "users"("age")`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "remove uniqueness from column without option",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "age", Type: field.TypeInt},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("age", "bigint", "NO", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0).
						AddRow("users_age_key", "age", "f", "t", 0))
				mock.ExpectCommit()
			},
		},
		{
			name: "remove uniqueness from column with option",
			tables: []*Table{
				{
					Name: "users",
					Columns: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "age", Type: field.TypeInt},
					},
					PrimaryKey: []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
					},
				},
			},
			options: []MigrateOption{WithDropIndex(true)},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "NO", "NULL").
						AddRow("age", "bigint", "NO", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0).
						AddRow("users_age_key", "age", "f", "t", 0))
				mock.ExpectQuery(escape(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS WHERE "table_schema" = CURRENT_SCHEMA() AND "constraint_type" = $1 AND "constraint_name" = $2`)).
					WithArgs("UNIQUE", "users_age_key").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				mock.ExpectExec(escape(`ALTER TABLE "users" DROP CONSTRAINT "users_age_key"`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "add edge to table",
			tables: func() []*Table {
				var (
					c1 = []*Column{
						{Name: "id", Type: field.TypeInt, Increment: true},
						{Name: "name", Type: field.TypeString, Nullable: true},
						{Name: "spouse_id", Type: field.TypeInt, Nullable: true},
					}
					t1 = &Table{
						Name:       "users",
						Columns:    c1,
						PrimaryKey: c1[0:1],
						ForeignKeys: []*ForeignKey{
							{
								Symbol:     "user_spouse" + strings.Repeat("_", 64), // super long fk.
								Columns:    c1[2:],
								RefColumns: c1[0:1],
								OnDelete:   Cascade,
							},
						},
					}
				)
				t1.ForeignKeys[0].RefTable = t1
				return []*Table{t1}
			}(),
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("users", true)
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "YES", "NULL").
						AddRow("name", "character", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				mock.fkExists("user_spouse____________________390ed76f91d3c57cd3516e7690f621dc", false)
				mock.ExpectExec(escape(`ALTER TABLE "users" ADD COLUMN "spouse_id" bigint NULL`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.fkExists("user_spouse____________________390ed76f91d3c57cd3516e7690f621dc", false)
				mock.ExpectExec(`ALTER TABLE "users" ADD CONSTRAINT ".{63}" FOREIGN KEY\("spouse_id"\) REFERENCES "users"\("id"\) ON DELETE CASCADE`).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "universal id for all tables",
			tables: []*Table{
				NewTable("users").AddPrimary(&Column{Name: "id", Type: field.TypeInt, Increment: true}),
				NewTable("groups").AddPrimary(&Column{Name: "id", Type: field.TypeInt, Increment: true}),
			},
			options: []MigrateOption{WithGlobalUniqueID(true)},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("ent_types", false)
				// create ent_types table.
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "ent_types"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, "type" varchar UNIQUE NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.tableExists("users", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "users"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				// set users id range.
				mock.ExpectExec(escape(`INSERT INTO "ent_types" ("type") VALUES ($1)`)).
					WithArgs("users").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("ALTER TABLE users ALTER COLUMN id RESTART WITH 1").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.tableExists("groups", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "groups"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				// set groups id range.
				mock.ExpectExec(escape(`INSERT INTO "ent_types" ("type") VALUES ($1)`)).
					WithArgs("groups").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("ALTER TABLE groups ALTER COLUMN id RESTART WITH 4294967296").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "universal id for new tables",
			tables: []*Table{
				NewTable("users").AddPrimary(&Column{Name: "id", Type: field.TypeInt, Increment: true}),
				NewTable("groups").AddPrimary(&Column{Name: "id", Type: field.TypeInt, Increment: true}),
			},
			options: []MigrateOption{WithGlobalUniqueID(true)},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("ent_types", true)
				// query ent_types table.
				mock.ExpectQuery(`SELECT "type" FROM "ent_types" ORDER BY "id" ASC`).
					WillReturnRows(sqlmock.NewRows([]string{"type"}).AddRow("users"))
				// query users table.
				mock.tableExists("users", true)
				// users table has no changes.
				mock.ExpectQuery(escape(`SELECT "column_name", "data_type", "is_nullable", "column_default" FROM INFORMATION_SCHEMA.COLUMNS WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
					WithArgs("users").
					WillReturnRows(sqlmock.NewRows([]string{"column_name", "data_type", "is_nullable", "column_default"}).
						AddRow("id", "bigint", "YES", "NULL"))
				mock.ExpectQuery(escape(fmt.Sprintf(indexesQuery, "users"))).
					WillReturnRows(sqlmock.NewRows([]string{"index_name", "column_name", "primary", "unique", "seq_in_index"}).
						AddRow("users_pkey", "id", "t", "t", 0))
				// query groups table.
				mock.tableExists("groups", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "groups"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				// set groups id range.
				mock.ExpectExec(escape(`INSERT INTO "ent_types" ("type") VALUES ($1)`)).
					WithArgs("groups").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("ALTER TABLE groups ALTER COLUMN id RESTART WITH 4294967296").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "universal id for restored tables",
			tables: []*Table{
				NewTable("users").AddPrimary(&Column{Name: "id", Type: field.TypeInt, Increment: true}),
				NewTable("groups").AddPrimary(&Column{Name: "id", Type: field.TypeInt, Increment: true}),
			},
			options: []MigrateOption{WithGlobalUniqueID(true)},
			before: func(mock pgMock) {
				mock.start("120000")
				mock.tableExists("ent_types", true)
				// query ent_types table.
				mock.ExpectQuery(`SELECT "type" FROM "ent_types" ORDER BY "id" ASC`).
					WillReturnRows(sqlmock.NewRows([]string{"type"}).AddRow("users"))
				// query and create users (restored table).
				mock.tableExists("users", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "users"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				// set users id range (without inserting to ent_types).
				mock.ExpectExec("ALTER TABLE users ALTER COLUMN id RESTART WITH 1").
					WillReturnResult(sqlmock.NewResult(0, 1))
				// query groups table.
				mock.tableExists("groups", false)
				mock.ExpectExec(escape(`CREATE TABLE IF NOT EXISTS "groups"("id" bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL, PRIMARY KEY("id"))`)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				// set groups id range.
				mock.ExpectExec(escape(`INSERT INTO "ent_types" ("type") VALUES ($1)`)).
					WithArgs("groups").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("ALTER TABLE groups ALTER COLUMN id RESTART WITH 4294967296").
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			tt.before(pgMock{mock})
			migrate, err := NewMigrate(sql.OpenDB("postgres", db), tt.options...)
			require.NoError(t, err)
			err = migrate.Create(context.Background(), tt.tables...)
			require.Equal(t, tt.wantErr, err != nil, err)
		})
	}
}

type pgMock struct {
	sqlmock.Sqlmock
}

func (m pgMock) start(version string) {
	m.ExpectBegin()
	m.ExpectQuery(escape("SHOW server_version_num")).
		WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(version))
}

func (m pgMock) tableExists(table string, exists bool) {
	count := 0
	if exists {
		count = 1
	}
	m.ExpectQuery(escape(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE "table_schema" = CURRENT_SCHEMA() AND "table_name" = $1`)).
		WithArgs(table).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func (m pgMock) fkExists(fk string, exists bool) {
	count := 0
	if exists {
		count = 1
	}
	m.ExpectQuery(escape(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS WHERE "table_schema" = CURRENT_SCHEMA() AND "constraint_type" = $1 AND "constraint_name" = $2`)).
		WithArgs("FOREIGN KEY", fk).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

package core

import (
	"context"

	"ganigeorgiev.com/pocketbase/dbx"
)

var _ dbx.Builder = (*contextDBBuilder)(nil)

// contextDBBuilder is a [dbx.Builder] wrapper that associates
// its given context by default to all builder query operations.
//
// Note that this is intended to be used as a lighter version of dbx.DB.WithContext()
// as the only thing it copies is the pointer to the existing Builder value and
// the context is attached lazily with each new user created query.
type contextDBBuilder struct {
	dbx.Builder
	ctx context.Context
}

// Select implements the [dbx.Builder.Select] interface method.
func (b *contextDBBuilder) Select(cols ...string) *dbx.SelectQuery {
	return b.Builder.Select(cols...).WithContext(b.ctx)
}

// Model implements the [dbx.Builder.Model] interface method.
func (b *contextDBBuilder) Model(data any) *dbx.ModelQuery {
	return b.Builder.Model(data).WithContext(b.ctx)
}

// Insert implements the [dbx.Builder.Insert] interface method.
func (b *contextDBBuilder) Insert(table string, cols dbx.Params) *dbx.Query {
	return b.Builder.Insert(table, cols).WithContext(b.ctx)
}

// Upsert implements the [dbx.Builder.Upsert] interface method.
func (b *contextDBBuilder) Upsert(table string, cols dbx.Params, constraints ...string) *dbx.Query {
	return b.Builder.Upsert(table, cols, constraints...).WithContext(b.ctx)
}

// Update implements the [dbx.Builder.Update] interface method.
func (b *contextDBBuilder) Update(table string, cols dbx.Params, where dbx.Expression) *dbx.Query {
	return b.Builder.Update(table, cols, where).WithContext(b.ctx)
}

// Delete implements the [dbx.Builder.Delete] interface method.
func (b *contextDBBuilder) Delete(table string, where dbx.Expression) *dbx.Query {
	return b.Builder.Delete(table, where).WithContext(b.ctx)
}

// CreateTable implements the [dbx.Builder.CreateTable] interface method.
func (b *contextDBBuilder) CreateTable(table string, cols map[string]string, options ...string) *dbx.Query {
	return b.Builder.CreateTable(table, cols, options...).WithContext(b.ctx)
}

// RenameTable implements the [dbx.Builder.RenameTable] interface method.
func (b *contextDBBuilder) RenameTable(oldName, newName string) *dbx.Query {
	return b.Builder.RenameTable(oldName, newName).WithContext(b.ctx)
}

// DropTable implements the [dbx.Builder.DropTable] interface method.
func (b *contextDBBuilder) DropTable(table string) *dbx.Query {
	return b.Builder.DropTable(table).WithContext(b.ctx)
}

// TruncateTable implements the [dbx.Builder.TruncateTable] interface method.
func (b *contextDBBuilder) TruncateTable(table string) *dbx.Query {
	return b.Builder.TruncateTable(table).WithContext(b.ctx)
}

// AddColumn implements the [dbx.Builder.AddColumn] interface method.
func (b *contextDBBuilder) AddColumn(table, col, typ string) *dbx.Query {
	return b.Builder.AddColumn(table, col, typ).WithContext(b.ctx)
}

// DropColumn implements the [dbx.Builder.DropColumn] interface method.
func (b *contextDBBuilder) DropColumn(table, col string) *dbx.Query {
	return b.Builder.DropColumn(table, col).WithContext(b.ctx)
}

// RenameColumn implements the [dbx.Builder.RenameColumn] interface method.
func (b *contextDBBuilder) RenameColumn(table, oldName, newName string) *dbx.Query {
	return b.Builder.RenameColumn(table, oldName, newName).WithContext(b.ctx)
}

// AlterColumn implements the [dbx.Builder.AlterColumn] interface method.
func (b *contextDBBuilder) AlterColumn(table, col, typ string) *dbx.Query {
	return b.Builder.AlterColumn(table, col, typ).WithContext(b.ctx)
}

// AddPrimaryKey implements the [dbx.Builder.AddPrimaryKey] interface method.
func (b *contextDBBuilder) AddPrimaryKey(table, name string, cols ...string) *dbx.Query {
	return b.Builder.AddPrimaryKey(table, name, cols...).WithContext(b.ctx)
}

// DropPrimaryKey implements the [dbx.Builder.DropPrimaryKey] interface method.
func (b *contextDBBuilder) DropPrimaryKey(table, name string) *dbx.Query {
	return b.Builder.DropPrimaryKey(table, name).WithContext(b.ctx)
}

// AddForeignKey implements the [dbx.Builder.AddForeignKey] interface method.
func (b *contextDBBuilder) AddForeignKey(table, name string, cols, refCols []string, refTable string, options ...string) *dbx.Query {
	return b.Builder.AddForeignKey(table, name, cols, refCols, refTable, options...).WithContext(b.ctx)
}

// DropForeignKey implements the [dbx.Builder.DropForeignKey] interface method.
func (b *contextDBBuilder) DropForeignKey(table, name string) *dbx.Query {
	return b.Builder.DropForeignKey(table, name).WithContext(b.ctx)
}

// CreateIndex implements the [dbx.Builder.CreateIndex] interface method.
func (b *contextDBBuilder) CreateIndex(table, name string, cols ...string) *dbx.Query {
	return b.Builder.CreateIndex(table, name, cols...).WithContext(b.ctx)
}

// CreateUniqueIndex implements the [dbx.Builder.CreateUniqueIndex] interface method.
func (b *contextDBBuilder) CreateUniqueIndex(table, name string, cols ...string) *dbx.Query {
	return b.Builder.CreateUniqueIndex(table, name, cols...).WithContext(b.ctx)
}

// DropIndex implements the [dbx.Builder.DropIndex] interface method.
func (b *contextDBBuilder) DropIndex(table, name string) *dbx.Query {
	return b.Builder.DropIndex(table, name).WithContext(b.ctx)
}

// NewQuery implements the [dbx.Builder.NewQuery] interface method.
func (b *contextDBBuilder) NewQuery(str string) *dbx.Query {
	return b.Builder.NewQuery(str).WithContext(b.ctx)
}

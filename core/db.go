package core

import (
	"errors"
	"fmt"
	"hash/crc32"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	validation "ganigeorgiev.com/pocketbase/ozzo-validation/v4"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/security"
	"github.com/spf13/cast"
)

const (
	idColumn string = "id"

	// DefaultIdLength is the default length of the generated model id.
	DefaultIdLength int = 15

	// DefaultIdAlphabet is the default characters set used for generating the model id.
	DefaultIdAlphabet string = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// DefaultIdRegex specifies the default regex pattern for an id value.
var DefaultIdRegex = regexp.MustCompile(`^\w+$`)

// DBExporter defines an interface for custom DB data export.
// Usually used as part of [App.Save].
type DBExporter interface {
	// DBExport returns a key-value map with the data to be used when saving the struct in the database.
	DBExport(app App) (map[string]any, error)
}

// PreValidator defines an optional model interface for registering a
// function that will run BEFORE firing the validation hooks (see [App.Validate]).
type PreValidator interface {
	// PreValidate defines a function that runs BEFORE the validation hooks.
	PreValidate(app App) error
}

// PostValidator defines an optional model interface for registering a
// function that will run AFTER executing the validation hooks (see [App.Validate]).
type PostValidator interface {
	// PostValidate defines a function that runs AFTER the successful
	// execution of the validation hooks.
	PostValidate(app App) error
}

// GenerateDefaultRandomId generates a default random id string
// (note: the generated random string is not intended for security purposes).
func GenerateDefaultRandomId() string {
	return security.PseudorandomStringWithAlphabet(DefaultIdLength, DefaultIdAlphabet)
}

// crc32Checksum generates a stringified crc32 checksum from the provided plain string.
func crc32Checksum(str string) string {
	return strconv.FormatInt(int64(crc32.ChecksumIEEE([]byte(str))), 10)
}

// ModelQuery creates a new preconfigured select data.db query with preset
// SELECT, FROM and other common fields based on the provided model.
func (app *BaseApp) ModelQuery(m Model) *dbx.SelectQuery {
	return app.modelQuery(app.ConcurrentDB(), m)
}

// AuxModelQuery creates a new preconfigured select auxiliary.db query with preset
// SELECT, FROM and other common fields based on the provided model.
func (app *BaseApp) AuxModelQuery(m Model) *dbx.SelectQuery {
	return app.modelQuery(app.AuxConcurrentDB(), m)
}

func (app *BaseApp) modelQuery(db dbx.Builder, m Model) *dbx.SelectQuery {
	tableName := m.TableName()

	return db.
		Select("{{" + tableName + "}}.*").
		From(tableName).
		WithBuildHook(func(query *dbx.Query) {
			query.WithExecHook(execLockRetry(app, defaultMaxLockRetries))
		})
}

// Delete deletes the specified model from the regular app database.
func (app *BaseApp) Delete(model Model) error {
	return app.delete(model, false)
}

// AuxDelete deletes the specified model from the auxiliary database.
func (app *BaseApp) AuxDelete(model Model) error {
	return app.delete(model, true)
}

func (app *BaseApp) delete(model Model, isForAuxDB bool) error {
	event := new(ModelEvent)
	event.App = app
	event.Type = ModelEventTypeDelete
	event.Model = model

	deleteErr := app.OnModelDelete().Trigger(event, func(e *ModelEvent) error {
		pk := cast.ToString(e.Model.LastSavedPK())
		if pk == "" {
			return errors.New("the model can be deleted only if it is existing and has a non-empty primary key")
		}

		// db write
		return e.App.OnModelDeleteExecute().Trigger(event, func(e *ModelEvent) error {
			var db dbx.Builder
			if isForAuxDB {
				db = e.App.AuxNonconcurrentDB()
			} else {
				db = e.App.NonconcurrentDB()
			}

			return baseLockRetry(func(attempt int) error {
				_, err := db.Delete(e.Model.TableName(), dbx.HashExp{
					idColumn: pk,
				}).Execute()

				return err
			}, defaultMaxLockRetries)
		})
	})
	if deleteErr != nil {
		errEvent := &ModelErrorEvent{ModelEvent: *event, Error: deleteErr}
		errEvent.App = app // replace with the initial app in case it was changed by the hook
		hookErr := app.OnModelAfterDeleteError().Trigger(errEvent)
		if hookErr != nil {
			return errors.Join(deleteErr, hookErr)
		}

		return deleteErr
	}

	if app.txInfo != nil {
		// execute later after the transaction has completed
		app.txInfo.OnComplete(func(txErr error) error {
			if app.txInfo != nil && app.txInfo.parent != nil {
				event.App = app.txInfo.parent
			}

			if txErr != nil {
				return app.OnModelAfterDeleteError().Trigger(&ModelErrorEvent{
					ModelEvent: *event,
					Error:      txErr,
				})
			}

			return app.OnModelAfterDeleteSuccess().Trigger(event)
		})
	} else if err := event.App.OnModelAfterDeleteSuccess().Trigger(event); err != nil {
		return err
	}

	return nil
}

// Save validates and saves the specified model into the regular app database.
//
// If you don't want to run validations, use [App.SaveNoValidate()].
func (app *BaseApp) Save(model Model) error {
	return app.save(model, true, false)
}

// SaveNoValidate saves the specified model into the regular app database without performing validations.
//
// If you want to also run validations before persisting, use [App.Save()].
func (app *BaseApp) SaveNoValidate(model Model) error {
	return app.save(model, false, false)
}

// AuxSave validates and saves the specified model into the auxiliary app database.
//
// If you don't want to run validations, use [App.AuxSaveNoValidate()].
func (app *BaseApp) AuxSave(model Model) error {
	return app.save(model, true, true)
}

// AuxSaveNoValidate saves the specified model into the auxiliary app database without performing validations.
//
// If you want to also run validations before persisting, use [App.AuxSave()].
func (app *BaseApp) AuxSaveNoValidate(model Model) error {
	return app.save(model, false, true)
}

// Validate triggers the OnModelValidate hook for the specified model.
func (app *BaseApp) Validate(model Model) error {
	if m, ok := model.(PreValidator); ok {
		if err := m.PreValidate(app); err != nil {
			return err
		}
	}

	event := new(ModelEvent)
	event.App = app
	event.Type = ModelEventTypeValidate
	event.Model = model

	return event.App.OnModelValidate().Trigger(event, func(e *ModelEvent) error {
		if m, ok := e.Model.(PostValidator); ok {
			if err := m.PostValidate(e.App); err != nil {
				return err
			}
		}

		return e.Next()
	})
}

// -------------------------------------------------------------------

func (app *BaseApp) save(model Model, withValidations bool, isForAuxDB bool) error {
	if model.IsNew() {
		return app.create(model, withValidations, isForAuxDB)
	}

	return app.update(model, withValidations, isForAuxDB)
}

func (app *BaseApp) create(model Model, withValidations bool, isForAuxDB bool) error {
	event := new(ModelEvent)
	event.App = app
	event.Type = ModelEventTypeCreate
	event.Model = model

	saveErr := app.OnModelCreate().Trigger(event, func(e *ModelEvent) error {
		// run validations (if any)
		if withValidations {
			validateErr := e.App.Validate(e.Model)
			if validateErr != nil {
				return validateErr
			}
		}

		// db write
		return e.App.OnModelCreateExecute().Trigger(event, func(e *ModelEvent) error {
			var db dbx.Builder
			if isForAuxDB {
				db = e.App.AuxNonconcurrentDB()
			} else {
				db = e.App.NonconcurrentDB()
			}

			dbErr := baseLockRetry(func(attempt int) error {
				if m, ok := e.Model.(DBExporter); ok {
					data, err := m.DBExport(e.App)
					if err != nil {
						return err
					}

					// manually add the id to the data if missing
					if _, ok := data[idColumn]; !ok {
						data[idColumn] = e.Model.PK()
					}

					if cast.ToString(data[idColumn]) == "" {
						return errors.New("empty primary key is not allowed when using the DBExporter interface")
					}

					_, err = db.Insert(e.Model.TableName(), data).Execute()

					return err
				}

				return db.Model(e.Model).Insert()
			}, defaultMaxLockRetries)
			if dbErr != nil {
				return dbErr
			}

			e.Model.MarkAsNotNew()

			return nil
		})
	})
	if saveErr != nil {
		event.Model.MarkAsNew() // reset "new" state

		errEvent := &ModelErrorEvent{ModelEvent: *event, Error: saveErr}
		errEvent.App = app // replace with the initial app in case it was changed by the hook
		hookErr := app.OnModelAfterCreateError().Trigger(errEvent)
		if hookErr != nil {
			return errors.Join(saveErr, hookErr)
		}

		return saveErr
	}

	if app.txInfo != nil {
		// execute later after the transaction has completed
		app.txInfo.OnComplete(func(txErr error) error {
			if app.txInfo != nil && app.txInfo.parent != nil {
				event.App = app.txInfo.parent
			}

			if txErr != nil {
				event.Model.MarkAsNew() // reset "new" state

				return app.OnModelAfterCreateError().Trigger(&ModelErrorEvent{
					ModelEvent: *event,
					Error:      txErr,
				})
			}

			return app.OnModelAfterCreateSuccess().Trigger(event)
		})
	} else if err := event.App.OnModelAfterCreateSuccess().Trigger(event); err != nil {
		return err
	}

	return nil
}

func (app *BaseApp) update(model Model, withValidations bool, isForAuxDB bool) error {
	event := new(ModelEvent)
	event.App = app
	event.Type = ModelEventTypeUpdate
	event.Model = model

	saveErr := app.OnModelUpdate().Trigger(event, func(e *ModelEvent) error {
		// run validations (if any)
		if withValidations {
			validateErr := e.App.Validate(e.Model)
			if validateErr != nil {
				return validateErr
			}
		}

		// db write
		return e.App.OnModelUpdateExecute().Trigger(event, func(e *ModelEvent) error {
			var db dbx.Builder
			if isForAuxDB {
				db = e.App.AuxNonconcurrentDB()
			} else {
				db = e.App.NonconcurrentDB()
			}

			return baseLockRetry(func(attempt int) error {
				if m, ok := e.Model.(DBExporter); ok {
					data, err := m.DBExport(e.App)
					if err != nil {
						return err
					}

					// note: for now disallow primary key change for consistency with dbx.ModelQuery.Update()
					if data[idColumn] != e.Model.LastSavedPK() {
						return errors.New("primary key change is not allowed")
					}

					_, err = db.Update(e.Model.TableName(), data, dbx.HashExp{
						idColumn: e.Model.LastSavedPK(),
					}).Execute()

					return err
				}

				return db.Model(e.Model).Update()
			}, defaultMaxLockRetries)
		})
	})
	if saveErr != nil {
		errEvent := &ModelErrorEvent{ModelEvent: *event, Error: saveErr}
		errEvent.App = app // replace with the initial app in case it was changed by the hook
		hookErr := app.OnModelAfterUpdateError().Trigger(errEvent)
		if hookErr != nil {
			return errors.Join(saveErr, hookErr)
		}

		return saveErr
	}

	if app.txInfo != nil {
		// execute later after the transaction has completed
		app.txInfo.OnComplete(func(txErr error) error {
			if app.txInfo != nil && app.txInfo.parent != nil {
				event.App = app.txInfo.parent
			}

			if txErr != nil {
				return app.OnModelAfterUpdateError().Trigger(&ModelErrorEvent{
					ModelEvent: *event,
					Error:      txErr,
				})
			}

			return app.OnModelAfterUpdateSuccess().Trigger(event)
		})
	} else if err := event.App.OnModelAfterUpdateSuccess().Trigger(event); err != nil {
		return err
	}

	return nil
}

func validateCollectionId(app App, optTypes ...string) validation.RuleFunc {
	return func(value any) error {
		id, _ := value.(string)
		if id == "" {
			return nil
		}

		collection := &Collection{}
		if err := app.ModelQuery(collection).Model(id, collection); err != nil {
			return validation.NewError("validation_invalid_collection_id", "Missing or invalid collection.")
		}

		if len(optTypes) > 0 && !slices.Contains(optTypes, collection.Type) {
			return validation.NewError(
				"validation_invalid_collection_type",
				fmt.Sprintf("Invalid collection type - must be %s.", strings.Join(optTypes, ", ")),
			)
		}

		return nil
	}
}

func validateRecordId(app App, collectionNameOrId string) validation.RuleFunc {
	return func(value any) error {
		id, _ := value.(string)
		if id == "" {
			return nil
		}

		collection, err := app.FindCachedCollectionByNameOrId(collectionNameOrId)
		if err != nil {
			return validation.NewError("validation_invalid_collection", "Missing or invalid collection.")
		}

		var exists int

		rowErr := app.ConcurrentDB().Select("(1)").
			From(collection.Name).
			AndWhere(dbx.HashExp{"id": id}).
			Limit(1).
			Row(&exists)

		if rowErr != nil || exists == 0 {
			return validation.NewError("validation_invalid_record", "Missing or invalid record.")
		}

		return nil
	}
}

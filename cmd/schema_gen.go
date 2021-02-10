package cmd

import (
	"ddbt/bigquery"
	"ddbt/config"
	"ddbt/fs"
	"ddbt/properties"

	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

func init() {
	rootCmd.AddCommand(schemaGenCmd)
}

var schemaGenCmd = &cobra.Command{
	Use:   "schema_gen [model name]",
	Short: "Generates the YML schema file for a given model",
	Args:  cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		modelName := args[0]

		// get filesystem, model and target
		fileSystem, _ := compileAllModels()
		model := fileSystem.Model(modelName)

		target, err := model.GetTarget()
		if err != nil {
			fmt.Println("could not get target for schema")
			os.Exit(1)
		}
		fmt.Println("\n🎯 Target for retrieving schema:", target.ProjectID+"."+target.DataSet)

		// retrieve columns from BigQuery
		bqColumns, err := GetColumnsForModel(modelName, target)
		if err != nil {
			fmt.Println("Could not retrieve schema")
			os.Exit(1)
		}
		fmt.Println("✅ BQ Schema retrieved. Number of columns in BQ table:", len(bqColumns))

		// create schema file
		ymlPath, schemaFile := generateEmptySchemaFile(model)

		if model.Schema == nil {
			fmt.Println("\n🔍 " + modelName + " schema file not found.. 🌱 Generating new schema file")
			schemaModel := generateNewSchemaModel(modelName, bqColumns)
			schemaFile.Models = properties.Models{&schemaModel}

			ymlBody := marshalYML(schemaFile)
			err = WriteYMLToFile(ymlPath, ymlBody)
			if err != nil {
				fmt.Println("Error writing YML to file in path")
				os.Exit(1)
			}
			fmt.Println("\n✅ " + modelName + "schema successfully written to path: " + ymlPath)

		} else {
			fmt.Println("\n🔍 " + modelName + " schema file found.. 🛠  Updating schema file")

			// set working schema model to current schema model
			schemaModel := model.Schema
			// add and remove columns in-place
			addMissingColumnsToSchema(schemaModel, bqColumns)
			removeOutdatedColumnsFromSchema(schemaModel, bqColumns)
			schemaFile.Models = properties.Models{schemaModel}

			ymlBody := marshalYML(schemaFile)
			err = WriteYMLToFile(ymlPath, ymlBody)
			if err != nil {
				fmt.Println("Error writing YML to file in path")
				os.Exit(1)
			}
			fmt.Println("\n✅ " + modelName + "schema successfully updated at path: " + ymlPath)
		}
	},
}

func marshalYML(schema interface{}) string {
	y, err := yaml.Marshal(&schema)
	if err != nil {
		fmt.Println("Could not marshal schema")
	}
	yml := fmt.Sprintf("\n%s\n\n", string(y))
	return yml
}

func GetColumnsForModel(modelName string, target *config.Target) ([]string, error) {
	schema, err := bigquery.GetColumnsFromTable(modelName, target)
	if err != nil {
		return nil, err
	}

	columns := []string{}
	for _, FieldSchema := range schema {
		column := fmt.Sprintf("%v", FieldSchema.Name)
		columns = append(columns, column)
	}
	return columns, nil
}

func WriteYMLToFile(filePath string, body string) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, _ = f.WriteString("# Auto-generated by DDBT at " + time.Now().String())
	_, err = f.WriteString(body)
	if err != nil {
		return err
	}
	return nil
}

// generate an empty schema file which will be populated according to existing yml schemas and the bigquery schema.
// Returns the local path for the yml file and the yml file struct
func generateEmptySchemaFile(model *fs.File) (ymlPath string, schemaFile properties.File) {
	ymlPath = strings.Replace(model.Path, ".sql", ".yml", 1)
	schemaFile = properties.File{}
	schemaFile.Version = properties.FileVersion
	return ymlPath, schemaFile
}

// generate a new schema model for the provided model name and bqcolumns
// this is used when then is no existing model
func generateNewSchemaModel(modelName string, bqColumns []string) properties.Model {
	schemaModel := properties.Model{}
	schemaModel.Name = modelName
	schemaModel.Description = "Please fill this in with a useful description.."
	schemaModel.Columns = []properties.Column{}
	for _, bqCol := range bqColumns {
		column := properties.Column{}
		column.Name = bqCol
		schemaModel.Columns = append(schemaModel.Columns, column)
	}

	return schemaModel
}

// check if bq column is in schema (add missing)
func addMissingColumnsToSchema(schemaModel *properties.Model, bqColumns []string) {
	columnsAdded := []string{}
	for _, bqCol := range bqColumns {
		columnFound := FindColumnInSchema(bqCol, schemaModel)
		if !columnFound {
			column := properties.Column{}
			column.Name = bqCol
			schemaModel.Columns = append(schemaModel.Columns, column)
			columnsAdded = append(columnsAdded, bqCol)
		}
	}
	fmt.Println("➕ Columns added to Schema (from BQ table):", columnsAdded)
}

// check if schema column in bq (remove missing)
func removeOutdatedColumnsFromSchema(schemaModel *properties.Model, bqColumns []string) {
	columnsRemoved := []string{}
	columnsKept := properties.Columns{}
	for _, schemaCol := range schemaModel.Columns {
		columnFound := FindSchemaColumnInSlice(schemaCol, bqColumns)
		if !columnFound {
			columnsRemoved = append(columnsRemoved, schemaCol.Name)
		} else {
			columnsKept = append(columnsKept, schemaCol)
		}
	}
	schemaModel.Columns = columnsKept
	fmt.Println("➖ Columns removed from Schema (no longer in BQ table):", columnsRemoved)
}

func FindColumnInSchema(column string, schema *properties.Model) bool {
	for _, schemacol := range schema.Columns {
		if schemacol.Name == column {
			return true
		}
	}
	return false
}

func FindSchemaColumnInSlice(column properties.Column, columnSlice []string) bool {
	for _, col := range columnSlice {
		if column.Name == col {
			return true
		}
	}
	return false
}
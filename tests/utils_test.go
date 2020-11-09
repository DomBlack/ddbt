package tests

import (
	"fmt"
	"testing"

	"ddbt/bigquery"
	"ddbt/compiler"
	"ddbt/compilerInterface"
	"ddbt/config"
	"ddbt/fs"
	"ddbt/jinja"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testVariables = map[string]*compilerInterface.Value{
	"table_name":       {StringValue: "BLAH"},
	"number_value":     {NumberValue: 1},
	"str_number_value": {StringValue: "2"},
	"map_object": {
		MapValue: map[string]*compilerInterface.Value{
			"string": {StringValue: "test"},
			"nested": {
				MapValue: map[string]*compilerInterface.Value{
					"number": {NumberValue: 3},
					"string": {StringValue: "FROM"},
				},
			},
			"key": {StringValue: "42"},
		},
	},
	"list_object": {
		ListValue: []*compilerInterface.Value{
			{StringValue: "first option is string"},
			{StringValue: "second option a string too!"},
			{StringValue: "third"},
			{MapValue: map[string]*compilerInterface.Value{
				"blah": {ListValue: []*compilerInterface.Value{
					{StringValue: "thingy"},
				}},
			}},
			{ListValue: []*compilerInterface.Value{
				{StringValue: "nested list test"},
				{NumberValue: 3},
			}},
		},
	},
}

var debugPrintAST = false

func CompileFromRaw(t *testing.T, raw string) (*fs.FileSystem, *compiler.GlobalContext, string) {
	fileSystem, err := fs.InMemoryFileSystem(
		map[string]string{
			"models/target_model.sql": raw,
		},
	)
	require.NoError(t, err, "Unable to construct in memory file system")

	for _, file := range fileSystem.AllFiles() {
		require.NoError(t, parseFile(file), "Unable to parse %s %s", file.Type, file.Name)
	}

	file := fileSystem.Model("target_model")
	require.NotNil(t, file, "Unable to extract the target_model from the In memory file system")
	require.NotNil(t, file.SyntaxTree, "target_model syntax tree is empty!")

	// Create the execution context
	config.GlobalCfg = &config.Config{
		Name: "Unit Test",
		Target: &config.Target{
			Name:      "unit_test",
			ProjectID: "unit_test_project",
			DataSet:   "unit_test_dataset",
			Location:  "US",
			Threads:   4,
		},
	}
	gc, err := compiler.NewGlobalContext(config.GlobalCfg, fileSystem)
	require.NoError(t, err, "Unable to create global context")

	macros := fileSystem.Macro("built-in-macros")
	require.NotNil(t, file, "Built in macros was nil")
	require.NoError(t, compiler.CompileModel(macros, gc, false), "Unable to compile built in macros")

	ec := compiler.NewExecutionContext(file, fileSystem, true, gc, gc)
	ec.SetVariable("config", file.ConfigObject())
	for key, value := range testVariables {
		ec.SetVariable(key, value)
	}

	finalAST, err := file.SyntaxTree.Execute(ec)
	require.NoError(t, err)
	require.NotNil(t, finalAST, "Output AST is nil")
	file.CompiledContents = finalAST.AsStringValue()

	return fileSystem, gc, bigquery.BuildQuery(file)
}

func assertCompileOutput(t *testing.T, expected, input string) {
	_, _, contentsOfModel := CompileFromRaw(t, input)

	assert.Equal(
		t,
		expected,
		contentsOfModel,
		"Unexpected output from %s",
		input,
	)
}

func parseFile(file *fs.File) error {
	syntaxTree, err := jinja.Parse(file)
	if err != nil {
		return err
	}

	if debugPrintAST {
		debugPrintAST = false
		fmt.Println(syntaxTree.String())
	}

	file.SyntaxTree = syntaxTree
	return nil
}

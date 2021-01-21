package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"ddbt/config"
	"ddbt/fs"
	"ddbt/utils"
)

func init() {
	rootCmd.AddCommand(isolateDAG)
	addModelsFlag(isolateDAG)
}

var isolateDAG = &cobra.Command{
	Use:   "isolate-dag",
	Short: "Creates a symlinked copy of the selected models, which can be then passed to Fishtown's DBT",
	Run: func(cmd *cobra.Command, args []string) {
		fileSystem, _ := compileAllModels()

		graph := buildGraph(fileSystem, ModelFilter) // Build the execution graph for the given command
		graph.AddReferencingTests()                  // And then add any tests which reference that graph

		if err := graph.AddAllUsedMacros(); err != nil {
			fmt.Printf("❌ Unable to get all used macros: %s\n", err)
			os.Exit(1)
		}

		isolateGraph(graph)
	},
}

func isolateGraph(graph *fs.Graph) {
	pb := utils.NewProgressBar("🔪 Isolating DAG", graph.Len())
	defer pb.Stop()

	// Create a temporary directory to stick the isolated models in
	isolationDir, err := ioutil.TempDir(os.TempDir(), "isolated-dag-")
	if err != nil {
		fmt.Printf("❌ Unable to create temporarily directory for DAG isolation: %s\n", err)
		os.Exit(1)
	}

	// Get the current working directory
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("❌ Unable to get working directory: %s\n", err)
		os.Exit(1)
	}

	symLink := func(pathInProject string) error {
		fullOrgPath := filepath.Join(cwd, pathInProject)
		symlinkedPath := filepath.Join(isolationDir, pathInProject)

		// Create the folder in the isolated dir if needed
		err := os.MkdirAll(filepath.Dir(symlinkedPath), os.ModePerm)
		if err != nil {
			return err
		}

		// Symlink the file in there
		err = os.Symlink(fullOrgPath, symlinkedPath)
		if err != nil {
			return err
		}

		return nil
	}

	// Create a blank file which DBT can read
	touch := func(pathInProject string) error {
		symlinkedPath := filepath.Join(isolationDir, pathInProject)

		// Create the folder in the isolated dir if needed
		err := os.MkdirAll(filepath.Dir(symlinkedPath), os.ModePerm)
		if err != nil {
			return err
		}

		// If the file doesn't exist create it with no contents
		if _, err := os.Stat(symlinkedPath); os.IsNotExist(err) {
			file, err := os.Create(symlinkedPath)
			if err != nil {
				return err
			}
			return file.Close()
		}

		return nil
	}

	projectFiles := []string{
		"dbt_project.yml",
		"ddbt_config.yml",
		"profiles",
		"debug",
		"docs",
		"dbt_modules",
	}

	// If we have a model groups file bring that too
	if config.GlobalCfg.ModelGroupsFile != "" {
		projectFiles = append(projectFiles, config.GlobalCfg.ModelGroupsFile)
	}

	for _, file := range projectFiles {
		if err := symLink(file); err != nil && !os.IsNotExist(err) {
			pb.Stop()
			fmt.Printf("❌ Unable to isolate project file `%s`: %s\n", file, err)
			os.Exit(1)
		}
	}

	err = graph.Execute(func(file *fs.File) error {
		// Symlink the file from the DAG into the isolated folder
		if err := symLink(file.Path); err != nil {
			pb.Stop()
			fmt.Printf("❌ Unable to isolate %s `%s`: %s\n", file.Type, file.Name, err)
			return err
		}

		// Symlink the schema if it exists
		schemaFile := strings.TrimSuffix(file.Path, filepath.Ext(file.Path)) + ".yml"
		if _, err := os.Stat(schemaFile); file.Schema != nil && err == nil {
			if err := symLink(schemaFile); err != nil {
				pb.Stop()
				fmt.Printf("❌ Unable to isolate schema for %s `%s`: %s\n", file.Type, file.Name, err)
				return err
			}
		}

		// Ensure usptream models are handled
		for _, upstream := range file.Upstreams() {
			if graph.Contains(upstream) {
				continue
			}

			switch upstream.Type {
			case fs.ModelFile:
				// Model's outside of the DAG but referenced by it need to exist for DBT to be able to run on this DAG
				// even if we run with the upstream command
				if err := touch(upstream.Path); err != nil {
					pb.Stop()
					fmt.Printf("❌ Unable to touch %s `%s`: %s\n", upstream.Type, upstream.Name, err)
					return err
				}

			default:
				// Any other than a model which is being used _should_ already be in the graph
				pb.Stop()
				fmt.Printf("❌ Unexpected Upstream %s `%s`\n", upstream.Type, upstream.Name)
				return err
			}
		}

		pb.Increment()
		return nil
	}, config.NumberThreads(), pb)

	if err != nil {
		os.Exit(1)
	}

	pb.Stop()

	fmt.Print(isolationDir)
}

package schemaTestMacros

import "fmt"

func Test_not_null_macro(project string, dataset string, model string, column_name string) (string, string) {
	return fmt.Sprintf(`select count(*) 
	from %s.%s.%s where %s is null
	`, project, dataset, model, column_name), "not_null"
}

package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const (
	tagRex = `gorethink:"([a-zA-Z0-9\-\_]*)[,]?(omidempty)?[a-zA-Z0-9\-\_\,]*"`
)

func getFieldType(field ast.Expr) string {
	switch v := field.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		if i, ok := v.X.(*ast.Ident); ok {
			return "*" + i.Name
		} else if i, ok := v.X.(*ast.SelectorExpr); ok {
			if in, ok := i.X.(*ast.Ident); ok {
				return "*" + in.Name + "." + i.Sel.Name
			}

		}
	case *ast.SelectorExpr:
		if i, ok := v.X.(*ast.Ident); ok {
			return i.Name + "." + v.Sel.Name
		}
	case *ast.MapType:
		return "map[" + getFieldType(v.Key) + "]" + getFieldType(v.Value)
	case *ast.SliceExpr:
		return "[]" + getFieldType(v.X)
	}
	return ""
}

func getFieldTag(field *ast.Field) (rqlgenRes [2]string) {
	rawTag := strings.Trim(field.Tag.Value, "`")
	out := rexp.FindStringSubmatch(rawTag)
	rqlgenRes[0] = out[1]
	rqlgenRes[1] = out[2]
	return rqlgenRes
}

var rexp *regexp.Regexp

type Field struct {
	Name          string
	DBName        string
	Type          string
	Omidempty     bool
	IsPolymorphic bool
}

type Struct struct {
	Fields []*Field
}

var (
	pFileName  = flag.String("file", "", "file name")
	pTypeName  = flag.String("type", "", "type name")
	pPolyField = flag.String("poly", "", "polymorphic field")
)

func main() {
	flag.Parse()
	rexp = regexp.MustCompile(tagRex)
	var FileName = *pFileName
	var TypeName = *pTypeName
	var PolymorphicField = *pPolyField
	var genFileName = strings.TrimSuffix(FileName, ".go") + "_rqlgen.go"
	fset := token.NewFileSet()
	aFile, err := parser.ParseFile(fset, FileName, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	strct := new(Struct)
	obj := aFile.Scope.Lookup(TypeName)
	if obj.Kind == ast.Typ {
		if t, ok := obj.Decl.(*ast.TypeSpec); ok {
			if s, ok := t.Type.(*ast.StructType); ok {
				for _, field := range s.Fields.List {
					f := new(Field)
					f.Name = field.Names[0].Name
					tags := getFieldTag(field)
					f.DBName = tags[0]
					f.Omidempty = len(tags[1]) != 0
					f.Type = getFieldType(field.Type)
					f.IsPolymorphic = f.Name == PolymorphicField
					strct.Fields = append(strct.Fields, f)
				}

			}
		}
	}
	gen, err := os.Create(genFileName)
	defer func() {
		gen.Close()
		exec.Command("goimports", "-w", genFileName).Run()
		exec.Command("gofmt", "-s", "-w", genFileName).Run()
	}()
	if err != nil {
		panic(err)
	}
	shType := strings.ToLower(string(TypeName[0]))
	fmt.Fprintf(gen, "package %v\n", aFile.Name.Name)
	fmt.Fprintf(gen, "import (\n")
	for _, imp := range aFile.Imports {
		if imp.Name != nil {
			fmt.Fprintf(gen, "\t%v %v\n", imp.Name.Name, imp.Path.Value)
		} else {
			fmt.Fprintf(gen, "\t%v\n", imp.Path.Value)
		}

	}
	fmt.Fprintf(gen, ")\n\n")
	fmt.Fprintf(gen, "func (%v *%v) MarshalRQL() (rqlgenRes interface{}, err error) {\n", shType, TypeName)
	fmt.Fprintf(gen, "\trqlgenTmp := make(map[string]interface{}, %v)\n", len(strct.Fields))
	for _, field := range strct.Fields {
		fmt.Fprintf(gen, "\trqlgenTmp[\"%v\"] = %v.%v\n", field.DBName, shType, field.Name)
	}
	fmt.Fprintf(gen, "\treturn rqlgenTmp, nil\n")
	fmt.Fprintf(gen, "}\n")

	fmt.Fprintf(gen, "\nfunc (%v *%v) UnmarshalRQL(rqlgenIface interface{}) ( err error) {\n", shType, TypeName)
	fmt.Fprintf(gen, "\trqlgenTmp, ok := rqlgenIface.(map[string]interface{})\n")
	fmt.Fprintf(gen, "\tif !ok {\n")
	fmt.Fprintf(gen, "\t\treturn err\n")
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\tvar comp bool\n")
	for _, field := range strct.Fields {
		fmt.Fprintf(gen, "\tgenrql%v, ok := rqlgenTmp[\"%v\"]\n", field.Name, field.DBName)
		fmt.Fprintf(gen, "\tif ok {\n")
		fmt.Fprintf(gen, "\t\t%v.%v, comp = genrql%[2]v.(%v)\n", shType, field.Name, field.Type)
		fmt.Fprintf(gen, "\t\tif !comp {\n")
		fmt.Fprintf(gen, "\t\t\treturn err\n")
		fmt.Fprintf(gen, "\t\t}\n")
		if !field.Omidempty {
			fmt.Fprintf(gen, "\t} else {\n")
			fmt.Fprintf(gen, "\t\treturn err\n")
		}
		fmt.Fprintf(gen, "\t}\n")
		if field.IsPolymorphic {
			fmt.Fprintf(gen, "\t%v.mutate()\n", shType)
		}
	}
	fmt.Fprintf(gen, "\treturn nil\n")
	fmt.Fprintf(gen, "}\n")
}

package main

import (
	"fmt"
	"io"
)

type Struct struct {
	Fields    []*Field
	Name      string
	ShortName string
}

func (strct *Struct) writeMarshal(gen io.Writer) {
	fmt.Fprintf(gen, "func (%v *%v) MarshalRQL() (rqlgenRes interface{}, err error) {\n", strct.ShortName, strct.Name)
	fmt.Fprintf(gen, "\tif %v == nil {\n", strct.ShortName)
	fmt.Fprintf(gen, "\t\treturn nil, nil\n")
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\trqlgenTmp := make(map[string]interface{}, %v)\n", len(strct.Fields))
	for _, field := range strct.Fields {
		fmt.Fprintf(gen, field.getMarshalStr(strct))
	}
	fmt.Fprintf(gen, "\treturn rqlgenTmp, nil\n")
	fmt.Fprintf(gen, "}\n")
}

func (strct *Struct) writeUnmarshal(gen io.Writer) {
	fmt.Fprintf(gen, "\nfunc (%v *%v) UnmarshalRQL(rqlgenIface interface{}) ( err error) {\n", strct.ShortName, strct.Name)
	fmt.Fprintf(gen, "\tif %v == nil {\n", strct.ShortName)
	fmt.Fprintf(gen, "\t\t%v = new(%v)\n", strct.ShortName, strct.Name)
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\tvar rqlgenTmp map[string]interface{}\n")
	fmt.Fprintf(gen, "\tvar ok bool\n")
	fmt.Fprintf(gen, "\tvar genrql interface{}\n")
	fmt.Fprintf(gen, "\tif rqlgenIface == nil {\n")
	fmt.Fprintf(gen, "\treturn nil\n")
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\trqlgenTmp, ok = rqlgenIface.(map[string]interface{})\n")
	fmt.Fprintf(gen, "\tif !ok {\n")
	fmt.Fprintf(gen, "\t\treturn genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(rqlgenIface).String())\n")
	fmt.Fprintf(gen, "\t}\n")
	for _, field := range strct.Fields {
		fmt.Fprintf(gen, field.getUnmarshalStr(strct))
	}
	fmt.Fprintf(gen, "\treturn nil\n")
	fmt.Fprintf(gen, "}\n")
}

package executor

import (
	"fmt"

	"github.com/chris-ramon/graphql-go/errors"
	"github.com/chris-ramon/graphql-go/language/ast"
	"github.com/chris-ramon/graphql-go/types"
	"github.com/kr/pretty"
)

type ExecuteParams struct {
	Schema        types.GraphQLSchema
	Root          map[string]interface{}
	AST           *ast.Document
	OperationName string
	Args          map[string]interface{}
}

func Execute(p ExecuteParams, resultChan chan *types.GraphQLResult) {
	var errors []error
	var result types.GraphQLResult
	params := BuildExecutionCtxParams{
		Schema:        p.Schema,
		Root:          p.Root,
		AST:           p.AST,
		OperationName: p.OperationName,
		Args:          p.Args,
		Errors:        errors,
		Result:        &result,
		ResultChan:    resultChan,
	}
	exeContext := buildExecutionContext(params)
	pretty.Println("----> exeContext", exeContext, result, errors)
	if result.HasErrors() {
		return
	}
	eOperationParams := ExecuteOperationParams{
		ExecutionContext: exeContext,
		Root:             p.Root,
		Operation:        exeContext.Operation,
	}
	executeOperation(eOperationParams, resultChan)
}

type ExecuteOperationParams struct {
	ExecutionContext ExecutionContext
	Root             map[string]interface{}
	Operation        ast.Definition
}

func executeOperation(p ExecuteOperationParams, r chan *types.GraphQLResult) {
	//TODO: mutation operation
	pretty.Println("---->", p)
	operationType := getOperationRootType(p.ExecutionContext.Schema, p.Operation, r)
	collectFieldsParams := CollectFieldsParams{
		ExeContext:    p.ExecutionContext,
		OperationType: operationType,
		SelectionSet:  p.Operation.GetSelectionSet(),
	}
	fields := collectFields(collectFieldsParams)
	executeFieldsParams := ExecuteFieldsParams{
		ExecutionContext: p.ExecutionContext,
		ParentType:       operationType,
		Source:           p.Root,
		Fields:           fields,
	}
	executeFields(executeFieldsParams, r)
}

func getOperationRootType(schema types.GraphQLSchema, operation ast.Definition, r chan *types.GraphQLResult) (objType types.GraphQLObjectType) {
	switch operation.GetOperation() {
	case "query":
		return schema.GetQueryType()
	case "mutation":
		mutationType := schema.GetMutationType()
		if mutationType.Name != "" {
			var result types.GraphQLResult
			err := graphqlerrors.NewGraphQLFormattedError("Schema is not configured for mutations")
			result.Errors = append(result.Errors, err)
			r <- &result
			return objType
		}
		return mutationType
	default:
		var result types.GraphQLResult
		err := graphqlerrors.NewGraphQLFormattedError("Can only execute queries and mutations")
		result.Errors = append(result.Errors, err)
		r <- &result
		return objType
	}
}

type CollectFieldsParams struct {
	ExeContext           ExecutionContext
	OperationType        types.GraphQLObjectType
	SelectionSet         *ast.SelectionSet
	Fields               map[string][]*ast.Field
	VisitedFragmentNames map[string]bool
}

func collectFields(p CollectFieldsParams) (r map[string][]*ast.Field) {

	return r
}

type ExecuteFieldsParams struct {
	ExecutionContext ExecutionContext
	ParentType       types.GraphQLObjectType
	Source           map[string]interface{}
	Fields           map[string][]*ast.Field
}

func executeFields(p ExecuteFieldsParams, resultChan chan *types.GraphQLResult) {
	var result types.GraphQLResult
	//mutable := reflect.ValueOf(p.ExecutionContext.Result.Data).Elem()
	//mutable.FieldByName("Name").SetString("R2-D2")
	resultChan <- &result
}

type BuildExecutionCtxParams struct {
	Schema        types.GraphQLSchema
	Root          map[string]interface{}
	AST           *ast.Document
	OperationName string
	Args          map[string]interface{}
	Errors        []error
	Result        *types.GraphQLResult
	ResultChan    chan *types.GraphQLResult
}

type ExecutionContext struct {
	Schema         types.GraphQLSchema
	Fragments      map[string]ast.Definition
	Root           map[string]interface{}
	Operation      ast.Definition
	VariableValues map[string]interface{}
	Errors         []error
}

func buildExecutionContext(p BuildExecutionCtxParams) (eCtx ExecutionContext) {
	operations := map[string]ast.Definition{}
	fragments := map[string]ast.Definition{}
	for _, statement := range p.AST.Definitions {
		switch stm := statement.(type) {
		case *ast.OperationDefinition:
			key := ""
			if stm.GetName() != nil && stm.GetName().Value != "" {
				key = stm.GetName().Value
			}
			pretty.Println("kinds.OperationDefinition", key)
			operations[key] = stm
		case *ast.FragmentDefinition:
			key := ""
			if stm.GetName() != nil && stm.GetName().Value != "" {
				key = stm.GetName().Value
			}
			pretty.Println("kinds.FragmentDefinition", key)
			fragments[key] = stm
		default:
			pretty.Println("default")
			err := graphqlerrors.NewGraphQLFormattedError(
				fmt.Sprintf("GraphQL cannot execute a request containing a %v", statement.GetKind()),
			)
			p.Result.Errors = append(p.Result.Errors, err)
			p.ResultChan <- p.Result
			return eCtx
		}
	}
	if (p.OperationName == "") && (len(operations) != 1) {
		err := graphqlerrors.NewGraphQLFormattedError("Must provide operation name if query contains multiple operations")
		p.Result.Errors = append(p.Result.Errors, err)
		p.ResultChan <- p.Result
		pretty.Println("err1")
		return eCtx
	}
	opName := p.OperationName
	if opName == "" {
		// get first opName
		for k, _ := range operations {
			pretty.Println("k", k)
			opName = k
			break
		}
	}
	operation, found := operations[opName]
	if !found {
		err := graphqlerrors.NewGraphQLFormattedError(fmt.Sprintf(`Unknown operation named "%v".`, opName))
		p.Result.Errors = append(p.Result.Errors, err)
		p.ResultChan <- p.Result
		pretty.Println("err2")
		return eCtx
	}
	variableValues, err := GetVariableValues(p.Schema, operation.GetVariableDefinitions(), p.Args)
	if err != nil {
		p.Result.Errors = append(p.Result.Errors, graphqlerrors.FormatError(err))
		p.ResultChan <- p.Result
		pretty.Println("err3")
		return eCtx
	}

	eCtx.Schema = p.Schema
	eCtx.Fragments = fragments
	eCtx.Root = p.Root
	eCtx.Operation = operation
	eCtx.VariableValues = variableValues
	eCtx.Errors = p.Errors
	return eCtx
}

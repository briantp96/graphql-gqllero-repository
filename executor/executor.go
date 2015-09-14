package executor

import (
	"fmt"

	"github.com/chris-ramon/graphql-go/errors"
	"github.com/chris-ramon/graphql-go/language/ast"
	"github.com/chris-ramon/graphql-go/types"
	"reflect"
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
	if p.Operation.GetOperation() == "mutation" {
		return
	}
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

// Extracts the root type of the operation from the schema.
func getOperationRootType(schema types.GraphQLSchema, operation ast.Definition, r chan *types.GraphQLResult) (objType *types.GraphQLObjectType) {
	if operation == nil {
		var result types.GraphQLResult
		err := graphqlerrors.NewGraphQLFormattedError("Can only execute queries and mutations")
		result.Errors = append(result.Errors, err)
		r <- &result
		return objType
	}
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
	OperationType        *types.GraphQLObjectType
	SelectionSet         *ast.SelectionSet
	Fields               map[string][]*ast.Field
	VisitedFragmentNames map[string]bool
}

// Given a selectionSet, adds all of the fields in that selection to
// the passed in map of fields, and returns it at the end.
func collectFields(p CollectFieldsParams) map[string][]*ast.Field {

	fields := p.Fields
	if fields == nil {
		fields = map[string][]*ast.Field{}
	}

	if p.SelectionSet == nil {
		return fields
	}

	for _, iSelection := range p.SelectionSet.Selections {
		switch selection := iSelection.(type) {
		case *ast.Field:
			if !shouldIncludeNode(p.ExeContext, selection.Directives) {
				continue
			}
			name := getFieldEntryKey(selection)
			if _, ok := fields[name]; !ok {
				fields[name] = []*ast.Field{}
			}
			fields[name] = append(fields[name], selection)
		case *ast.InlineFragment:
			if !shouldIncludeNode(p.ExeContext, selection.Directives) ||
				!doesFragmentConditionMatch(p.ExeContext, selection, p.OperationType) {
				continue
			}
			innerParams := CollectFieldsParams{
				ExeContext:           p.ExeContext,
				OperationType:        p.OperationType,
				SelectionSet:         selection.SelectionSet,
				Fields:               fields,
				VisitedFragmentNames: p.VisitedFragmentNames,
			}
			fields = collectFields(innerParams)
		case *ast.FragmentSpread:
			fragName := ""
			if selection.Name != nil {
				fragName = selection.Name.Value
			}
			if _, ok := p.VisitedFragmentNames[fragName]; !ok ||
				!shouldIncludeNode(p.ExeContext, selection.Directives) {
				continue
			}
			p.VisitedFragmentNames[fragName] = true
			fragment, hasFragment := p.ExeContext.Fragments[fragName]
			if !hasFragment {
				continue
			}
			switch fragment := fragment.(type) {
			case *ast.FragmentDefinition:
				if !shouldIncludeNode(p.ExeContext, fragment.Directives) ||
					!doesFragmentConditionMatch(p.ExeContext, fragment, p.OperationType) {
					continue
				}
				innerParams := CollectFieldsParams{
					ExeContext:           p.ExeContext,
					OperationType:        p.OperationType,
					SelectionSet:         fragment.GetSelectionSet(),
					Fields:               fields,
					VisitedFragmentNames: p.VisitedFragmentNames,
				}
				fields = collectFields(innerParams)
			}
		}
	}
	return fields
}

type ExecuteFieldsParams struct {
	ExecutionContext ExecutionContext
	ParentType       *types.GraphQLObjectType
	Source           map[string]interface{}
	Fields           map[string][]*ast.Field
}

// Implements the "Evaluating selection sets" section of the spec for "read" mode.
func executeFields(p ExecuteFieldsParams, resultChan chan *types.GraphQLResult) {
	if p.Source == nil {
		p.Source = map[string]interface{}{}
	}
	if p.Fields == nil {
		p.Fields = map[string][]*ast.Field{}
	}
	var result types.GraphQLResult

	finalResults := map[string]interface{}{}
	for responseName, fieldASTs := range p.Fields {
		result := resolveField(p.ExecutionContext, p.ParentType, p.Source, fieldASTs)
		if result != nil {
			finalResults[responseName] = result
		}
	}

	result.Data = finalResults
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
			operations[key] = stm
		case *ast.FragmentDefinition:
			key := ""
			if stm.GetName() != nil && stm.GetName().Value != "" {
				key = stm.GetName().Value
			}
			fragments[key] = stm
		default:
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
		return eCtx
	}
	opName := p.OperationName
	if opName == "" {
		// get first opName
		for k, _ := range operations {
			opName = k
			break
		}
	}
	operation, found := operations[opName]
	if !found {
		err := graphqlerrors.NewGraphQLFormattedError(fmt.Sprintf(`Unknown operation named "%v".`, opName))
		p.Result.Errors = append(p.Result.Errors, err)
		p.ResultChan <- p.Result
		return eCtx
	}
	variableValues, err := getVariableValues(p.Schema, operation.GetVariableDefinitions(), p.Args)
	if err != nil {
		p.Result.Errors = append(p.Result.Errors, graphqlerrors.FormatError(err))
		p.ResultChan <- p.Result
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

// Implements the logic to compute the key of a given field’s entry
func getFieldEntryKey(node *ast.Field) string {
	if node.Alias != nil && node.Alias.Value != "" {
		return node.Alias.Value
	}
	if node.Name != nil && node.Name.Value != "" {
		return node.Name.Value
	}
	return ""
}

// Determines if a field should be included based on the @include and @skip
// directives, where @skip has higher precedence than @include.
func shouldIncludeNode(eCtx ExecutionContext, directives []*ast.Directive) bool {
	//TODO: shouldIncludeNode not implemented
	return true
}

// Determines if a fragment is applicable to the given type.
func doesFragmentConditionMatch(eCtx ExecutionContext, fragment ast.Node, ttype *types.GraphQLObjectType) bool {
	//TODO: doesFragmentConditionMatch not implemented
	return true
}

/**
 * Resolves the field on the given source object. In particular, this
 * figures out the value that the field returns by calling its resolve function,
 * then calls completeValue to complete promises, serialize scalars, or execute
 * the sub-selection-set for objects.
 */
func resolveField(eCtx ExecutionContext, parentType *types.GraphQLObjectType, source map[string]interface{}, fieldASTs []*ast.Field) interface{} {

	fieldAST := fieldASTs[0]
	fieldName := getFieldEntryKey(fieldAST)

	fieldDef := getFieldDef(eCtx.Schema, parentType, fieldName)
	if fieldDef == nil {
		return nil
	}
	returnType := fieldDef.Type
	resolveFn := fieldDef.Resolve

	if resolveFn == nil {
		resolveFn = defaultResolveFn
	}

	// Build a map of arguments from the field.arguments AST, using the
	// variables scope to fulfill any variable references.
	// TODO: find a way to memoize, in case this field is within a List type.
	args, _ := getArgumentValues(fieldDef.Args, fieldAST.Arguments, eCtx.VariableValues)

	// The resolve function's optional third argument is a collection of
	// information about the current execution state.
	info := types.GraphQLResolveInfo{
		FieldName:      fieldName,
		FieldASTs:      fieldASTs,
		ReturnType:     returnType,
		ParentType:     parentType,
		Schema:         eCtx.Schema,
		Fragments:      eCtx.Fragments,
		RootValue:      eCtx.Root,
		Operation:      eCtx.Operation,
		VariableValues: eCtx.VariableValues,
	}

	// TODO: If an error occurs while calling the field `resolve` function, ensure that
	// it is wrapped as a GraphQLError with locations. Log this error and return
	// null if allowed, otherwise throw the error so the parent field can handle
	// it.
	result := resolveFn(types.GQLFRParams{
		Source: source,
		Args:   args,
		Info:   info,
	})
	return result
}

func getFieldDef(schema types.GraphQLSchema, parentType *types.GraphQLObjectType, fieldName string) *types.GraphQLFieldDefinition {

	if parentType == nil {
		return nil
	}

	if fieldName == types.SchemaMetaFieldDef.Name &&
		schema.GetQueryType().Name == parentType.Name {
		return types.SchemaMetaFieldDef
	}
	if fieldName == types.TypeMetaFieldDef.Name &&
		schema.GetQueryType().Name == parentType.Name {
		return types.TypeMetaFieldDef
	}
	if fieldName == types.TypeNameMetaFieldDef.Name &&
		schema.GetQueryType().Name == parentType.Name {
		return types.TypeNameMetaFieldDef
	}
	return parentType.GetFields()[fieldName]
}

func defaultResolveFn(p types.GQLFRParams) interface{} {
	property := p.Source[p.Info.FieldName]
	val := reflect.ValueOf(property)
	if val.IsValid() && val.Type().Kind() == reflect.Func {
		// try type casting the func to the most basic func signature
		// for more complex signatures, user have to define ResolveFn
		if propertyFn, ok := property.(func() interface{}); ok {
			return propertyFn()
		}
	}
	return property
}

package printer_test

import (
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/chris-ramon/graphql-go"
	"github.com/chris-ramon/graphql-go/language/ast"
	"github.com/chris-ramon/graphql-go/language/printer"
)

func parse(t *testing.T, query string) *ast.Document {
	astDoc, err := graphql.Parse(graphql.ParseParams{
		Source: query,
		Options: graphql.ParseOptions{
			NoLocation: true,
		},
	})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return astDoc
}

func TestPrinter_DoesNotAlterAST(t *testing.T) {
	b, err := ioutil.ReadFile("../../kitchen-sink.graphql")
	if err != nil {
		t.Fatalf("unable to load kitchen-sink.graphql")
	}

	query := string(b)
	astDoc := parse(t, query)

	astDocBefore := graphql.ASTToJSON(t, astDoc)

	_ = printer.Print(astDoc)

	astDocAfter := graphql.ASTToJSON(t, astDoc)

	_ = graphql.ASTToJSON(t, astDoc)

	if !reflect.DeepEqual(astDocAfter, astDocBefore) {
		t.Fatalf("Unexpected result, Diff: %v", graphql.Diff(astDocAfter, astDocBefore))
	}
}

func TestPrinter_PrintsMinimalAST(t *testing.T) {
	astDoc := ast.NewField(&ast.Field{
		Name: ast.NewName(&ast.Name{
			Value: "foo",
		}),
	})
	results := printer.Print(astDoc)
	expected := "foo"
	if !reflect.DeepEqual(results, expected) {
		t.Fatalf("Unexpected result, Diff: %v", graphql.Diff(expected, results))
	}
}

func TestPrinter_PrintsKitchenSink(t *testing.T) {
	b, err := ioutil.ReadFile("../../kitchen-sink.graphql")
	if err != nil {
		t.Fatalf("unable to load kitchen-sink.graphql")
	}

	query := string(b)
	astDoc := parse(t, query)
	expected := `query namedQuery($foo: ComplexFooType, $bar: Bar = DefaultBarValue) {
  customUser: user(id: [987, 654]) {
    id
    ... on User @defer {
      field2 {
        id
        alias: field1(first: 10, after: $foo) @include(if: $foo) {
          id
          ...frag
        }
      }
    }
  }
}

mutation favPost {
  fav(post: 123) @defer {
    post {
      id
    }
  }
}

fragment frag on Follower {
  foo(size: $size, bar: $b, obj: {key: "value"})
}

{
  unnamed(truthyVal: true, falseyVal: false)
  query
}
`
	results := printer.Print(astDoc)

	if !reflect.DeepEqual(expected, results) {
		t.Fatalf("Unexpected result, Diff: %v", graphql.Diff(results, expected))
	}
}

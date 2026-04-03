package ddb

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Client wraps the DynamoDB API used by the CLI.
type Client struct {
	api    *dynamodb.Client
	Region string
}

// New creates a Client using the same profile/region rules as the ECS client.
func New(profile, region string) (*Client, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &Client{
		api:    dynamodb.NewFromConfig(cfg),
		Region: cfg.Region,
	}, nil
}

// ListTables returns all table names in the account/region (sorted).
func (c *Client) ListTables(ctx context.Context) ([]string, error) {
	var names []string
	p := dynamodb.NewListTablesPaginator(c.api, &dynamodb.ListTablesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		names = append(names, page.TableNames...)
	}
	sort.Strings(names)
	return names, nil
}

// FilterTablesByKeyword keeps tables whose name contains keyword (case-insensitive).
func FilterTablesByKeyword(tables []string, keyword string) []string {
	if keyword == "" {
		return tables
	}
	k := strings.ToLower(keyword)
	var out []string
	for _, t := range tables {
		if strings.Contains(strings.ToLower(t), k) {
			out = append(out, t)
		}
	}
	return out
}

// KeySchema holds partition and optional sort key metadata from DescribeTable.
type KeySchema struct {
	PartitionName string
	PartitionType string // S, N, or B
	SortName      string
	SortType      string
}

// DescribeKeySchema loads the table key schema.
func (c *Client) DescribeKeySchema(ctx context.Context, table string) (*KeySchema, error) {
	out, err := c.api.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(table),
	})
	if err != nil {
		return nil, err
	}
	attrTypes := map[string]types.ScalarAttributeType{}
	for _, a := range out.Table.AttributeDefinitions {
		attrTypes[aws.ToString(a.AttributeName)] = a.AttributeType
	}
	var ks KeySchema
	for _, elt := range out.Table.KeySchema {
		name := aws.ToString(elt.AttributeName)
		t, ok := attrTypes[name]
		if !ok {
			return nil, fmt.Errorf("missing attribute type for %q", name)
		}
		ts := string(t)
		switch elt.KeyType {
		case types.KeyTypeHash:
			ks.PartitionName = name
			ks.PartitionType = ts
		case types.KeyTypeRange:
			ks.SortName = name
			ks.SortType = ts
		}
	}
	if ks.PartitionName == "" {
		return nil, fmt.Errorf("no partition key in table %q", table)
	}
	return &ks, nil
}

// QueryInput carries string values from the user; types follow KeySchema (S/N/B).
type QueryInput struct {
	Table    string
	PKName   string
	PKType   string
	PKValue  string
	SKName   string
	SKType   string
	SKValue  string // ignored if SKName empty
	MaxItems int32
}

// Query runs a Query with equality on partition key and optional sort key.
func (c *Client) Query(ctx context.Context, in QueryInput) (string, error) {
	if in.MaxItems <= 0 {
		in.MaxItems = 25
	}
	pkVal, err := attributeValueFromString(types.ScalarAttributeType(in.PKType), in.PKValue)
	if err != nil {
		return "", fmt.Errorf("partition key: %w", err)
	}
	expr := "#pk = :pk"
	names := map[string]string{"#pk": in.PKName}
	vals := map[string]types.AttributeValue{":pk": pkVal}

	if in.SKName != "" {
		skVal, err := attributeValueFromString(types.ScalarAttributeType(in.SKType), in.SKValue)
		if err != nil {
			return "", fmt.Errorf("sort key: %w", err)
		}
		expr += " AND #sk = :sk"
		names["#sk"] = in.SKName
		vals[":sk"] = skVal
	}

	out, err := c.api.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(in.Table),
		KeyConditionExpression:    aws.String(expr),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: vals,
		Limit:                     aws.Int32(in.MaxItems),
	})
	if err != nil {
		return "", err
	}

	items := make([]map[string]any, 0, len(out.Items))
	for _, av := range out.Items {
		items = append(items, attributeMapToJSONable(av))
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func attributeValueFromString(t types.ScalarAttributeType, s string) (types.AttributeValue, error) {
	s = strings.TrimSpace(s)
	switch t {
	case types.ScalarAttributeTypeS:
		return &types.AttributeValueMemberS{Value: s}, nil
	case types.ScalarAttributeTypeN:
		if s == "" {
			return nil, fmt.Errorf("empty number")
		}
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return nil, fmt.Errorf("invalid number %q", s)
		}
		return &types.AttributeValueMemberN{Value: s}, nil
	case types.ScalarAttributeTypeB:
		return nil, fmt.Errorf("binary partition/sort keys are not supported in v1")
	default:
		return nil, fmt.Errorf("unsupported type %v", t)
	}
}

func attributeMapToJSONable(m map[string]types.AttributeValue) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = attributeValueToJSONable(v)
	}
	return out
}

func attributeValueToJSONable(v types.AttributeValue) any {
	switch x := v.(type) {
	case *types.AttributeValueMemberS:
		return x.Value
	case *types.AttributeValueMemberN:
		return x.Value
	case *types.AttributeValueMemberBOOL:
		return x.Value
	case *types.AttributeValueMemberNULL:
		return nil
	case *types.AttributeValueMemberL:
		s := make([]any, len(x.Value))
		for i, item := range x.Value {
			s[i] = attributeValueToJSONable(item)
		}
		return s
	case *types.AttributeValueMemberM:
		return attributeMapToJSONable(x.Value)
	default:
		return fmt.Sprintf("%v", v)
	}
}

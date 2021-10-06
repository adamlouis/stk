package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

type Stack struct {
	Region       string            `yaml:"Region"`
	Template     string            `yaml:"Template"`
	Parameters   map[string]string `yaml:"Parameters"`
	Capabilities []string          `yaml:"Capabilities"`
}

type CFNParams struct {
	cloudformation.CreateStackInput
}

var rootCmd = &cobra.Command{
	Use:   "stk",
	Short: "stk: manage cloudformation stacks",
	Long:  `stk: manage cloudformation stacks`,
}

var createCmd = &cobra.Command{
	Use:   "create [stack]",
	Short: "create a new cloudformation stack",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := confirmAccount()
		if err != nil {
			panic(err)
		}
		if !c {
			return
		}

		svc, params, err := getCFNParams(cmd, args)
		if err != nil {
			panic(err)
		}

		if !yn(fmt.Sprintf("create stack %s in region %s?", *params.StackName, *svc.Config.Region)) {
			return
		}

		out, err := svc.CreateStack(&cloudformation.CreateStackInput{
			StackName:    params.StackName,
			Parameters:   params.Parameters,
			TemplateBody: params.TemplateBody,
			Capabilities: params.Capabilities,
		})

		if err != nil {
			panic(err)
		}
		fmt.Printf("created stack %s\n", *out.StackId)
	},
}

var updateCmd = &cobra.Command{
	Use:   "update [stack]",
	Short: "update an existing cloudformation stack",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := confirmAccount()
		if err != nil {
			return err
		}
		if !c {
			return nil
		}

		svc, params, err := getCFNParams(cmd, args)
		if err != nil {
			return err
		}

		name := aws.String(fmt.Sprintf("stk-%d", time.Now().Unix()))
		chc, err := svc.CreateChangeSet(&cloudformation.CreateChangeSetInput{
			ChangeSetName: name,
			StackName:     params.StackName,
			Parameters:    params.Parameters,
			TemplateBody:  params.TemplateBody,
			Capabilities:  params.Capabilities,
		})
		if err != nil {
			return err
		}

		err = svc.WaitUntilChangeSetCreateComplete(&cloudformation.DescribeChangeSetInput{
			ChangeSetName: name,
			StackName:     params.StackName,
		})
		if err != nil {
			return err
		}

		// TODO: print change set details
		if !yn(fmt.Sprintf("execute change set %s in region %s?", *name, *svc.Config.Region)) {
			return nil
		}

		_, err = svc.ExecuteChangeSet(&cloudformation.ExecuteChangeSetInput{
			ChangeSetName: name,
			StackName:     params.StackName,
		})
		if err != nil {
			return err
		}

		fmt.Printf("executed change set on %s\n", *chc.StackId)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().String("template-dir", "", "")
	rootCmd.PersistentFlags().String("stack-file", "", "")
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(updateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
func confirmAccount() (bool, error) {
	sess := session.Must(session.NewSession())
	stssvc := sts.New(sess)
	sout, err := stssvc.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return false, err
	}

	accountID := *sout.Account

	iamsvc := iam.New(sess)
	iout, err := iamsvc.ListAccountAliases(&iam.ListAccountAliasesInput{})
	if err != nil {
		return false, err
	}

	aliases := make([]string, len(iout.AccountAliases))
	for i, a := range iout.AccountAliases {
		aliases[i] = *a
	}

	return yn(fmt.Sprintf("continue with AWS account %s %s?", accountID, strings.Join(aliases, ", "))), nil
}

func yn(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s (y/n): ", prompt)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text) == "y"
}

func getCFNParams(cmd *cobra.Command, args []string) (*cloudformation.CloudFormation, *CFNParams, error) {
	if len(args) < 1 {
		return nil, nil, fmt.Errorf("no stack arg provided")
	}

	stackFile, _ := cmd.Flags().GetString("stack-file")
	templateDir, _ := cmd.Flags().GetString("template-dir")
	stackName := args[0]

	stackDefs, err := getStackDefinitions(stackFile)
	if err != nil {
		return nil, nil, err
	}

	stack, ok := stackDefs[stackName]
	if !ok {
		return nil, nil, err
	}

	cs := make([]*string, len(stack.Capabilities))
	for i, c := range stack.Capabilities {
		cs[i] = aws.String(c)
	}

	ps := []*cloudformation.Parameter{}
	for k, v := range stack.Parameters {
		k := k
		v := v
		ps = append(ps, &cloudformation.Parameter{
			ParameterKey:   &k,
			ParameterValue: &v,
		})
	}

	tpath, err := filepath.Abs(templateDir + "/" + stack.Template)
	if err != nil {
		return nil, nil, err
	}
	tbody, err := ioutil.ReadFile(tpath)
	if err != nil {
		return nil, nil, err
	}
	tbodys := string(tbody)

	if stack.Region == "" {
		return nil, nil, fmt.Errorf("no region provided")
	}

	svc := cloudformation.New(session.Must(
		session.NewSession()),
		aws.NewConfig().WithRegion(stack.Region),
	)

	return svc, &CFNParams{
		cloudformation.CreateStackInput{
			StackName:    &stackName,
			Parameters:   ps,
			Capabilities: cs,
			TemplateBody: &tbodys,
		},
	}, nil
}

func getStackDefinitions(path string) (map[string]Stack, error) {
	filename, _ := filepath.Abs(path)
	yamlFile, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("could not read stack definition file  %s", filename)
	}
	stacks := map[string]Stack{}
	err = yaml.Unmarshal(yamlFile, &stacks)
	if err != nil {
		return nil, err
	}
	return stacks, nil
}

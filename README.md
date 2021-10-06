# stk
define &amp; release cloudformation stacks

1) define cloudformation tempaltes as yml
2) define cloudformation stacks as yml (parameters, credentials, region, etc)
3) create stack ... or creat & execute cloudformation change set to update

* y/n confirmation to confirm correct AWS account
* y/n confirmation before create or update

`stk create [stack] --stack-file="..." --template-dir="..."`
`stk update [stack] --stack-file="..." --template-dir="..."`



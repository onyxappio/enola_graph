package graphsession

import (
 "fmt"
 "context"
 "github.com/enola-labs/enola/internal/graphstream"
 "testing"
)

func TestIndependentAliasContextNoopAfterEveryTransition(t *testing.T) {
 for _, subpath := range []bool{false,true} {
  t.Run(fmt.Sprintf("subpath=%v",subpath),func(t *testing.T){
   spec:="@local/new";exports:=`{".":"./first/index.ts"}`
   if subpath { spec+="/feature";exports=`{"./feature":"./first/feature.ts"}` }
   files:=map[string]string{"package.json":`{"name":"root"}`,"consumer.ts":"import { target } from '"+spec+"';\nexport const use = target;\n"}
   for i:=0;i<8;i++ {files[fmt.Sprintf("isolated/file%d.ts",i)]=fmt.Sprintf("export const isolated%d=%d;\n",i,i)}
   root:=setupTSRepo(t,files);eng:=configScopeEngine(t,root);opts:=Options{StateDir:t.TempDir(),AuthoritativeFiles:true};cons:=NewConsumer()
   run:=func(want string){
    t.Helper();result,_,_:=configScopeRun(t,eng,root,opts,cons)
    quiet:=&graphstream.MemorySink{}
    noop,err:=Run(context.Background(),eng,root,quiet,opts);if err!=nil {t.Fatal(err)}
    if noop.ParsedFiles!=0 || noop.TargetGeneration!=result.TargetGeneration || len(quiet.CloneRecords())!=0 {t.Fatalf("immediate no-op parsed=%d events=%d generation=%d->%d",noop.ParsedFiles,len(quiet.CloneRecords()),result.TargetGeneration,noop.TargetGeneration)}
    assertAppliedEqualsCold(t,cons,coldConsumer(t,eng,root))
    if want!="" { target:=independentDiscoveryAliasTarget(t,cons,"consumer.ts");found:=false;for _,n:=range cons.Owners[ownerKey(want)] {if n.ID==target {found=true}};if !found {t.Fatalf("import target %s not owned by %s",target,want)} }
   }
   run("")
   entry:="index.ts";if subpath {entry="feature.ts"}
   writeFile(t,root,"packages/new/first/"+entry,"export const target=1;\n")
   writeFile(t,root,"packages/new/second/"+entry,"export const target=2;\n")
   writeFile(t,root,"packages/new/package.json",`{"name":"@local/new","exports":`+exports+`}`)
   run("packages/new/first/"+entry)
   exports=`{".":"./second/index.ts"}`;if subpath {exports=`{"./*":"./first/*.ts","./feature":"./second/feature.ts"}`}
   writeFile(t,root,"packages/new/package.json",`{"name":"@local/new","exports":`+exports+`}`)
   run("packages/new/second/"+entry)
   // The old sources remain; only the package name disappears from resolution.
   writeFile(t,root,"packages/new/package.json",`{"name":"@local/renamed","exports":`+exports+`}`)
   run("")
  })
 }
}

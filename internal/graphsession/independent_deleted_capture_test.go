package graphsession

import (
 "context"
 "errors"
 "testing"
 "os"
 "path/filepath"
 "github.com/enola-labs/enola/internal/graphstream"
)

func TestIndependentDeletedCaptureAvoidsPublication(t *testing.T) {
 root:=setupTSRepo(t,map[string]string{"src/a.ts":"export const a=1;\n","src/use.ts":"import {a} from './a'; export const use=a;\n"})
 eng:=testEngine(t,root);ctx:=context.Background();opts:=Options{StateDir:t.TempDir(),AuthoritativeFiles:true}
 sink:=&graphstream.MemorySink{};resident,err:=OpenSession(ctx,eng,root,sink,opts);if err!=nil{t.Fatal(err)};defer resident.Close()
 initial,err:=resident.reconcile(ctx,false);if err!=nil{t.Fatal(err)}
 offset:=len(sink.CloneRecords())
 writeFile(t,root,"src/a.ts","export const a=2;\n")
 resident.mu.Lock()
 input,reason:=resident.contentInputs([]string{"src/a.ts"},&WorkCounters{})
 resident.mu.Unlock()
 if reason!=""||input==nil||string(input.sources["src/a.ts"])!="export const a=2;\n" {t.Fatalf("expected captured changed bytes: reason=%s",reason)}
 // The mutation is before transaction entry, with no deferred parse hook.
 if err := os.Remove(filepath.Join(root,"src/a.ts")); err != nil { t.Fatal(err) }
 resident.mu.Lock();_,_,err=resident.transaction(ctx,input,true);resident.mu.Unlock()
 if !errors.Is(err,ErrInputsChanged){t.Fatalf("want retryable supersession, got %v",err)}
 interrupted:=sink.CloneRecords()[offset:]
 _,_,ends,decodeErr:=DecodeRun(interrupted);if decodeErr!=nil{t.Fatal(decodeErr)}
 if len(ends)!=0 {t.Fatal("superseded attempt published successful End")}
 st,err:=loadCommittedState(opts.StateDir);if err!=nil{t.Fatal(err)}
 if st.Generation!=initial.TargetGeneration{t.Fatalf("supersession advanced generation to %d",st.Generation)}
 if len(interrupted)!=0{t.Errorf("already-superseded capture published %d records before detection",len(interrupted))}
 result,err:=resident.reconcile(ctx,false);if err!=nil{t.Fatal(err)}
 cons:=NewConsumer();applyRun(t,cons,sink);assertAppliedEqualsCold(t,cons,coldConsumer(t,eng,root))
 if result.TargetGeneration!=initial.TargetGeneration+1{t.Fatalf("retry generation=%d",result.TargetGeneration)}
}

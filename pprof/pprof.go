package pprof

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/trace"
)

func Pprof(port string) {
	runtime.MemProfileRate = 10 * 1024
	go func() {
		//close GC
		//debug.SetGCPercent(1)
		//run trace
		http.HandleFunc("/start", traces)
		//stop trace
		http.HandleFunc("/stop", traceStop)
		//handle GC
		http.HandleFunc("/gc", gc)
		//open  http server
		http.ListenAndServe(":"+port, nil)
	}()
}

func gc(w http.ResponseWriter, r *http.Request) {
	runtime.GC()
	w.Write([]byte("StartGC"))
}

func traces(w http.ResponseWriter, r *http.Request) {
	f, err := os.Create("trace.out")
	if err != nil {
		panic(err)
	}

	err = trace.Start(f)
	if err != nil {
		panic(err)
	}
	w.Write([]byte("TrancStart"))
	fmt.Println("StartTrancs")
}

func traceStop(w http.ResponseWriter, r *http.Request) {
	trace.Stop()
	w.Write([]byte("TrancStop"))
	fmt.Println("StopTrancs")
}

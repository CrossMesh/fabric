# Arbiter

## Intro

Arbiter is tracer to **manage lifecycle** of goroutines, **preventing risk of goroutine leak**. It also simplifies implement of **Graceful Termination**.



## Get started

```go
import arbit "github.com/sunmxt/utt/arbiter"
```

```go
arbiter := arbit.New() // new arbiter
```



### Spawn goroutine (like "go" keyword)

```go
arbiter.Go(func(){
  // ... do something ...
})
```

### Trace execution

```go
arbiter.Do(func(){
  // ... do something ...
})
```



### Shutdown

```go
arbiter.Shutdown() // shutdown. Arbiter will send exit signal to all goroutines and executions. 
```

### Intergrate Shutdown() with OS signals

```go
arbiter.StopOSSignals(os.Kill, os.Interrupt) // SIGKILL and SIGINT will tigger Shutdown().
```

### Watch a shutdown

*Shutdown* signal will be sent via a channel. use *select* to watch it.

```go
select {
  case <-arbiter.Exit(): // watch for a shutdown signal.
    // ...do cleanning...
  case ...
  case ...
}
```

Or you may periodically check **arbiter.Shutdown()**. For example: 

```go
for arbiter.ShouldRun() {
  // ... do something ...
}
// ...do cleanning...
```



### Join (Wait)

```go
arbiter.Join() // blocked until all goroutines and executions exited.
```

#### Arbit

```go
arbiter.Arbit() // Let SIGKILL and SIGINT tigger Shutdown() than wait.
```

---

### Arbiter tree

Create derived Arbiter.

```go
child := arbit.NewWithParent(arbiter) // new child arbiter
```

Many derived arbiters forms a **arbiter tree**, which has following properties:

- Derived arbiters will be **automatically shut down** when the parent does.
- *Arbiter.Join()* waits for all goroutines and executions on the arbiter tree (i.e **childrens' included** ) to exit


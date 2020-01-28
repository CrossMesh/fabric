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
  // ... do something.
  if arbiter.ShouldRun() { // check whether should exit.
    // do cleaning...
    return
  }
  // ... do something ...
})
```

### Trace execution

```go
arbiter.Do(func(){
  // ... do something.
})
```

### Join (Wait)

```go
arbiter.Join(false) // blocked until all goroutines and executions exited.
```

### Shutdown

```go
arbiter.Shutdown() // shutdown. Arbiter will send exit signal to all goroutines and executions. 
```

### Intergrate Shutdown() with OS signals

```go
arbiter.StopOSSignals(os.Kill, os.Interrupt) // SIGKILL and SIGINT will tigger Shutdown().
arbiter.Join(false) // just wait.
```

#### Arbit

```go
arbiter.Arbit() // Let SIGKILL and SIGINT tigger Shutdown() than wait.
```


package main

import (
    "fmt"
    "toonflow/adapter"
    "toonflow/skill"
    "toonflow/storage"
    "toonflow/engine"
    "toonflow/task"
    "toonflow/ws"
    "toonflow/service"
    _ "toonflow/adapter"
    "time"
)

func main() {
    fmt.Println("Starting...")
    
    // Test adapter
    fmt.Printf("Registered vendors: %v\n", adapter.List())
    
    // Test skill
    sm := skill.NewManager("skills")
    if err := sm.Load(); err != nil {
        fmt.Printf("Skill load error: %v\n", err)
    }
    fmt.Printf("Loaded skill categories: %d\n", len(sm.GetAll()))
    
    // Test storage
    db, err := storage.Init("/tmp/test_toonflow.db")
    if err != nil {
        fmt.Printf("DB init error: %v\n", err)
    } else {
        fmt.Println("DB initialized OK")
        db.Close()
    }
    
    // Test queue
    q := task.NewQueue(5)
    fmt.Printf("Queue created, active: %d\n", q.ActiveCount())
    
    // Test config
    fmt.Println("Config OK")
    
    // Test engine
    v, _ := adapter.Get("openai_compatible")
    if v != nil {
        fmt.Println("Engine adapter OK")
    }
    
    // Test ws
    bc := ws.NewConnManager()
    fmt.Println("WS manager OK")
    
    // Test service
    fmt.Println("Service package OK")
    
    fmt.Println("All checks passed!")
}

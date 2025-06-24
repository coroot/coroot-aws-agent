package utils

import (
    "sync"
)

type WorkerPool struct {
    workerCount int
    jobChannel  chan func()
    wg          sync.WaitGroup
}

func NewWorkerPool(workerCount int) *WorkerPool {
    pool := &WorkerPool{
        workerCount: workerCount,
        jobChannel:  make(chan func(), workerCount),
    }
    pool.start()
    return pool
}

func (p *WorkerPool) start() {
    for i := 0; i < p.workerCount; i++ {
        p.wg.Add(1)
        go func() {
            defer p.wg.Done()
            for job := range p.jobChannel {
                job()
            }
        }()
    }
}

func (p *WorkerPool) Submit(job func()) {
    p.jobChannel <- job
}

func (p *WorkerPool) Wait() {
    close(p.jobChannel)
    p.wg.Wait()
}
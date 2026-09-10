package conf

import (
	"fmt"
	"sync"
	"time"
)

const maxRecords = 1000

type stat struct {
	mu      sync.RWMutex
	records []bool
	index   int // 当前位置
	total   int // 已记录总数
	succ    int // 成功记录数
}

type info struct {
	Block string `json:"block"`
	Succ  string `json:"succ"`
	Time  int64  `json:"time"`
}

var (
	data sync.Map // map[string]*stat
	last sync.Map
)

func getStat(net string) *stat {
	val, _ := data.LoadOrStore(net, &stat{
		records: make([]bool, maxRecords),
	})
	return val.(*stat)
}

func RecordSuccess(net, block string) {
	s := getStat(net)
	s.mu.Lock()

	if s.total >= maxRecords && !s.records[s.index] {
		s.succ++
	} else if s.total < maxRecords {
		s.succ++
	}

	s.records[s.index] = true
	s.index = (s.index + 1) % maxRecords
	if s.total < maxRecords {
		s.total++
	}
	s.mu.Unlock()

	last.Store(net, info{Block: block, Succ: GetSuccessRate(net), Time: time.Now().Unix()})
}

func RecordFailure(net string) {
	s := getStat(net)
	s.mu.Lock()

	if s.total >= maxRecords && s.records[s.index] {
		s.succ--
	}

	s.records[s.index] = false
	s.index = (s.index + 1) % maxRecords
	if s.total < maxRecords {
		s.total++
	}
	s.mu.Unlock()

	if value, ok := last.Load(net); ok {
		current := value.(info)
		current.Succ = GetSuccessRate(net)
		current.Time = time.Now().Unix()
		last.Store(net, current)
	}
}

func GetStats() map[string]info {
	var m = make(map[string]info)
	last.Range(func(k, v interface{}) bool {
		m[k.(string)] = v.(info)

		return true
	})

	return m
}

func GetSuccessRate(net string) string {
	return fmt.Sprintf("%.2f%%", SuccessRate(net))
}

func SuccessRate(net string) float64 {
	s := getStat(net)
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.total == 0 {
		return 100
	}

	return float64(s.succ) / float64(s.total) * 100
}

func ResetStats(net string) {
	s := getStat(net)
	s.mu.Lock()
	s.records = make([]bool, maxRecords)
	s.index = 0
	s.total = 0
	s.succ = 0
	s.mu.Unlock()

	if value, ok := last.Load(net); ok {
		current := value.(info)
		current.Succ = GetSuccessRate(net)
		current.Time = time.Now().Unix()
		last.Store(net, current)
	}
}

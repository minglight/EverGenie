package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 等引擎的時限都在這裡（DEV-159，作者 2026-11-14：一位秘書那一側卡住，不應該讓整個看板跟著卡住）。
// 原則：一位秘書那一側不回，只有她自己的那件事等到時限、得到一句白話；別位秘書的收話、開、關、重開與定時那一輪都不等她。
// 數字怎麼來：正常要多久（看引擎套件裡寫死的等待）乘上幾倍的餘裕，不能短到正常情況被誤判。
// 引擎套件碰終端機的單次呼叫自己有 10 秒上限；這裡的時限是整個動作的上限。
type EngineLimits struct {
	// Deliver：貼一句話進她的對話並確認送出。正常：貼上後等 1 秒送出、最多再看 2 秒有沒有收下，加上擋住的選單要先取消再貼一次，
	// 大約 1～5 秒。逾時當成這次沒送到，照 SES-51 退避、用同一個封包編號再試（引擎先查上一次到了沒，不會貼成兩則）。
	Deliver time.Duration
	// Start：開一個對話。引擎開好終端機視窗就回（不等平台載完），正常 1～3 秒；同名的舊視窗要先收掉時多幾秒。
	Start time.Duration
	// Stop：關一個對話。正常不到 1 秒；收不掉時引擎自己會拖到單次呼叫的 10 秒上限。
	Stop time.Duration
	// Transcript：讀一段對話記錄（算未讀、記已讀）。讀的是本機的檔案，正常幾十毫秒；很長的對話整份讀也在 1 秒內。
	Transcript time.Duration
	// BootPrompt：問一個對話「有沒有卡在要主人出手的提示」。引擎只看自己記著的東西，正常幾毫秒；
	// 要比看板那一側整批的 2 秒（chat 的 bootPromptsTimeout）短，一位不回時別位的提示才來得及交回去。
	BootPrompt time.Duration
	// Answer：替主人在她的畫面上回答一題（代答）或按一個啟動提示。正常：按鍵後等 0.3 秒再看一次畫面，1～2 秒。
	Answer time.Duration
	// Command：對話指令（清掉、整理、換模型、換力度、叫她停下）。換模型要等平台確認最多 5 秒、清掉要等新對話最多 3 秒。
	Command time.Duration
	// AskGrace：一次性短問題（QuickAsk）在「呼叫的人給引擎的秒數」之外，核心多等這麼久收回覆；引擎沒給秒數時用它自己的 600 秒。
	AskGrace time.Duration
}

// DefaultEngineLimits 是正式的數字。
func DefaultEngineLimits() EngineLimits {
	return EngineLimits{
		Deliver:    20 * time.Second,
		Start:      30 * time.Second,
		Stop:       20 * time.Second,
		Transcript: 5 * time.Second,
		BootPrompt: time.Second,
		Answer:     15 * time.Second,
		Command:    30 * time.Second,
		AskGrace:   5 * time.Second,
	}
}

// askDefault：呼叫的人沒給秒數時引擎自己等多久（ENG-11：0＝600 秒）。
const askDefault = 600 * time.Second

// gaveUp：這次呼叫是我們設的時限到了才回來的（不是呼叫的人自己走了、也不是核心在停）。
func gaveUp(call, parent context.Context) bool {
	return call.Err() == context.DeadlineExceeded && parent.Err() == nil
}

// silent 是時限到了給主人看的話：誰、在做什麼、等了多久、接下來怎麼辦。
func silent(what string, waited time.Duration, next string) error {
	return platform.Unavailable(fmt.Sprintf("秘書那邊沒回應：%s等了 %d 秒沒有結果。%s", what, int(waited/time.Second), next))
}

// sessionLock：每個對話自己的一把鎖。開、關、重開要等引擎，同一個對話一次只做一件；不同對話互不相等。
// 拿著它的時候可以等引擎（有時限）；不拿著它去等別的對話的鎖。
func (m *Module) sessionLock(name string) *sync.Mutex {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	l := m.sessionLocks[name]
	if l == nil {
		l = &sync.Mutex{}
		m.sessionLocks[name] = l
	}
	return l
}

// spawn 在背景做一件要等引擎的事：定時那一輪與派送器把它交出去就往下走，不等。
// fn 拿到的 ctx 在呼叫的人走了、或對話服務要停的時候都會取消（停的時候不用等到引擎的時限）。
// 對話服務沒在跑（還沒啟動、正在停）就不做、回 false。不能拿著 m.mu 呼叫。
func (m *Module) spawn(ctx context.Context, fn func(context.Context)) bool {
	m.mu.Lock()
	if m.cancel == nil || m.ctx.Err() != nil {
		m.mu.Unlock()
		return false
	}
	running := m.ctx
	m.wg.Add(1) // 跟 Stop 的取消同在 m.mu 底下：Stop 開始等的時候這一筆已經算進去了
	m.mu.Unlock()
	m.bgMu.Lock()
	m.bgCount++
	m.bgMu.Unlock()
	go func() {
		defer m.wg.Done()
		defer func() {
			m.bgMu.Lock()
			m.bgCount--
			m.bgMu.Unlock()
		}()
		defer m.recoverWorker()
		bg, cancel := context.WithCancel(ctx)
		defer cancel()
		defer context.AfterFunc(running, cancel)()
		fn(bg)
	}()
	return true
}

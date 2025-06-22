package main

import (
	"bufio"     // https://pkg.go.dev/bufio
	"fmt"       // https://pkg.go.dev/fmt
	"net"       // https://pkg.go.dev/net
	"net/netip" // https://pkg.go.dev/net/netip
	"os"        // https://pkg.go.dev/os
	"os/exec"   // https://pkg.go.dev/os/exec
	"runtime"   // https://pkg.go.dev/runtime
	"strconv"   // https://pkg.go.dev/strconv
	"strings"   // https://pkg.go.dev/strings
	"sync"      // https://pkg.go.dev/sync
	"time"      // https://pkg.go.dev/time

	"github.com/go-ping/ping" // https://pkg.go.dev/github.com/go-ping/ping
)

// --------------------------------
// ヘルパー関数
// --------------------------------
// CLI画面をクリア
func clearScreen() {
	switch runtime.GOOS {
	case "windows":
		clearCmd := exec.Command("cmd", "/c", "cls")
		clearCmd.Stdout = os.Stdout
		clearCmd.Run()
	default:
		fmt.Print("\033c")
	}
}

// NICに付与されたIPv4プレフィックスを取得
func getNICipv4NetworkAddress() ([]string, error) {
	var ipv4NetAddrsPrefix []string

	// 自身の全ネットワークインターフェースを取得
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf(" Interfaces Error: %v", err)
	}

	for _, iface := range ifaces {
		// 無効なインターフェースとループバックを除外
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// インターフェースに紐づくアドレス(IPv4/IPv6)を取得
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			// IPv4アドレス以外を除外
			ipv4Prefix, err := netip.ParsePrefix(addr.String())
			if err != nil || !ipv4Prefix.Addr().Is4() {
				continue
			}

			// xxx.xxx.xxx.0/24 のIPv4ネットワークアドレスを取得
			ipv4NetAddrPrefix := ipv4Prefix.Masked()
			ipv4NetAddrsPrefix = append(ipv4NetAddrsPrefix, ipv4NetAddrPrefix.String())
		}
	}
	return ipv4NetAddrsPrefix, nil
}

// 疎通確認したいIPv4アドレスを選択し「1 - 254」のレンジを生成
func selectIPv4SweepRange(ipv4NetaddrsPrefix []string) ([]netip.Addr, error) {
	var input_munber int
	var ipv4Range []netip.Addr

	for {
		fmt.Println("\n 疎通確認したい番号を選択してください")
		fmt.Println("---------------------------------")

		for index, value := range ipv4NetaddrsPrefix {
			fmt.Println("", index, ":", value)
		}
		fmt.Println("---------------------------------")
		fmt.Print(" 番号：")

		// 入力された値の判定
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			input := scanner.Text()
			number, err := strconv.Atoi(input)
			if err != nil {
				clearScreen()
				fmt.Print("\n ※数字を選択してください")
				continue
			} else if len(ipv4NetaddrsPrefix)-1 < number {
				clearScreen()
				fmt.Print("\n ※選択した数字は対象外です")
				continue
			} else {
				input_munber = number
			}
		} else {
			clearScreen()
			fmt.Print("\n Input Error")
			continue
		}

		// xxx.xxx.xxx. のIPv4アドレスを取得
		octets := strings.Split(ipv4NetaddrsPrefix[input_munber], ".")
		ipv4ThreeOctet := strings.Join(octets[:3], ".") + "."

		// xxx.xxx.xxx.1-254 のIPv4アドレスの範囲を取得
		for count := 1; count < 255; count++ {
			ipv4Addr, err := netip.ParseAddr(ipv4ThreeOctet + strconv.Itoa(count))
			if err != nil {
				return nil, fmt.Errorf(" ParseAddr Error: %v", err)
			}
			ipv4Range = append(ipv4Range, ipv4Addr)
		}
		return ipv4Range, nil
	}
}

func probeOnce(sweepIPv4 netip.Addr) bool {
	// pingオブジェクトを作成
	pinger, err := ping.NewPinger(sweepIPv4.String())
	if err != nil {
		return false
	}

	switch runtime.GOOS {
	case "windows":
		// Windowsでは、RawソケットAPIが使えないので権モードを使わず Win32API (ICMP API) を利用する
		pinger.SetPrivileged(true)
	default:
		// Linux/macOSはfalseで通常ユーザーでも実行可能
		pinger.SetPrivileged(false)
	}

	// 送信回数
	pinger.Count = 1

	// タイムアウト時間
	pinger.Timeout = 1 * time.Second

	// PING実行
	err = pinger.Run()
	return err == nil && pinger.Statistics().PacketsRecv > 0
}

// 各IPv4アドレスにPINGを実行
func pingSweep(ipv4RangeChan <-chan netip.Addr, resultChan chan<- netip.Addr, wg *sync.WaitGroup) {
	defer wg.Done()

	for sweepIPv4 := range ipv4RangeChan {
		for count := 0; count < 2; count++ {
			if probeOnce(sweepIPv4) {
				resultChan <- sweepIPv4
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
}

// ゴルーチンでPINGを並行処理し、応答があるIPv4アドレスを表示
func parallelPingSweep(ipv4Range []netip.Addr) {
	var wg sync.WaitGroup
	ipv4RangeChan := make(chan netip.Addr, len(ipv4Range))
	resultChan := make(chan netip.Addr, len(ipv4Range))

	// CLI画面をクリア
	clearScreen()

	fmt.Println("\n 検出したIPv4アドレス")
	fmt.Println("---------------------------------")

	// 取得した論理CPUの数でスレッド数を決める
	threads := min(254, runtime.NumCPU()*20)

	// PINGを実行
	for count := 0; count < threads; count++ {
		wg.Add(1)
		go pingSweep(ipv4RangeChan, resultChan, &wg)
	}

	// IPv4アドレスを並列処理用のキューに格納
	for _, ipv4 := range ipv4Range {
		ipv4RangeChan <- ipv4
	}
	close(ipv4RangeChan)

	wg.Wait()
	close(resultChan)

	for ipv4 := range resultChan {
		fmt.Println(" 応答あり: ", ipv4)
	}

	fmt.Print("---------------------------------\n\n")
}

// --------------------------------
// Main関数
// --------------------------------
func main() {
	// CLI画面をクリア
	clearScreen()

	// NICに付与されたIPv4プレフィックスを取得
	ipv4NetaddrsPrefix, err := getNICipv4NetworkAddress()
	if err != nil {
		fmt.Println(err)
	}

	// 疎通確認したいIPv4アドレスを選択し「1 - 254」のレンジを生成
	ipv4Range, err := selectIPv4SweepRange(ipv4NetaddrsPrefix)
	if err != nil {
		fmt.Println(err)
	}

	// ゴルーチンでPINGを並行処理し、応答があるIPv4アドレスを表示
	parallelPingSweep(ipv4Range)
}

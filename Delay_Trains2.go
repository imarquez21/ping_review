/*
 *
 */

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	mpping "github.com/wontoniii/go-mp-ping_CaptureDelays"
)

type response struct {
	addr *net.IPAddr
	rtt  time.Duration
	seqn int
}

func ping(hostname, source string, useUDP, debug bool, count, interval, trainS, trainI, gamma int, rate float64, pattern []int) {
	p := mpping.NewPinger()
	if useUDP {
		p.Network("udp")
	}

	if debug {
		p.Debug = true
	}

	netProto := "ip4:icmp"
	if strings.Index(hostname, ":") != -1 {
		netProto = "ip6:ipv6-icmp"
	}
	ra, err := net.ResolveIPAddr(netProto, hostname)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if source != "" {
		p.Source(source)
	}

	if gamma > 0 {
		p.Gamma = time.Duration(gamma) * time.Millisecond
	}

	if rate > 0 {
		p.Rate = rate
	}

	if trainS > 1 {
		p.Train = true
		p.TrainSize = trainS
		if trainI > 0 {
			p.TrainInt = time.Duration(trainI) * time.Millisecond
		}
		/*
			if validateSpacing(trainS, trainI, interval, gamma) {

			} else {
				fmt.Printf("The inter-train spacing (-i) minus gamma (-g); must be bigger than the total inter-ping spacing within the train.\n")
				fmt.Printf("Current inter-train spacing value is, %v, gamma value is: %v. inter-train spacing minus gamma is: %v\n", interval, gamma, interval-gamma)
				fmt.Printf("Current total inter-ping spacing is (-t %v * -I %v): %v.\n", trainS, trainI, (trainS * trainI))
				os.Exit(1)
			}
		*/
	}

	if len(pattern) > 0 {
		p.SetPattern(pattern)
	}

	p.AddIPAddr(ra)

	onRecv, onIdle := make(chan *response), make(chan bool)
	p.OnRecv = func(addr *net.IPAddr, t time.Duration, seqn int) {
		onRecv <- &response{addr: addr, rtt: t, seqn: seqn}
	}
	p.OnIdle = func() {
		onIdle <- true
	}

	p.MaxRTT = time.Duration(interval) * time.Millisecond

	var sent, received, crec int
	var min, max, avg, mdev float32
	results := make([]float32, 0)
	fmt.Printf("PING %s (%s) %d(%d) bytes of data\n", hostname, ra.String(), 0, 0)
	st := time.Now()

	sent = trainS
	p.RunLoop()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)

loop:
	for {
		select {
		case <-c:
			break loop
		case res := <-onRecv:
			results = append(results, float32(res.rtt)/float32(time.Millisecond))
			received += 1
			crec += 1
			fmt.Printf("%d bytes from %s: icmp_seq=%d ttl=%d time=%.2f ms\n", p.Size+56, res.addr, res.seqn, 128, float32(res.rtt)/float32(time.Millisecond))
			if p.SentICMP/trainS >= count && received/trainS == count {
				fmt.Printf("\n")
				break loop
			}
		case <-onIdle:
			if p.SentICMP/trainS >= count {
				fmt.Printf("We Entered onIdle\n")
				break loop
			}
			sent += trainS
			if crec == 0 {
				fmt.Printf("%s : unreachable\n", hostname)
			} else {
				crec = 0
			}
		case <-p.Done():
			if err = p.Err(); err != nil {
				fmt.Println("Ping failed:", err)
			}
			fmt.Printf("We Entered on Done\n")
			break loop
		}
	}
	et := time.Now()
	elapsed := et.Sub(st)
	fmt.Printf("--- %s ping statistics ---\n", hostname)
	fmt.Printf("%d packets transmitted, %d received, %.2f%% packet loss, time %d secs\n", p.SentICMP, received, float32(p.SentICMP-received)/float32(p.SentICMP), int(elapsed/time.Second))
	//fmt.Printf("ICMP Messages sent: %d", p.SentICMP)
	lenResults := float32(len(results))
	if lenResults > 0 {
		min = 1000000
		max = -1
		avg = 0
		mdev = 0
		for _, val := range results {
			if val < min {
				min = val
			}
			if val > max {
				max = val
			}
			avg += val
		}
		avg = avg / lenResults
		for _, val := range results {
			mdev += (val - avg) * (val - avg)
		}
		mdev = mdev / lenResults
		fmt.Printf("rtt min/avg/max/mdev = %.3f/%.3f/%.3f/%.3f ms\n", min, avg, max, mdev)
	}

	//Sorting by sequence number the map which holds RTTS and Sequence numbers
	var keys []int
	for k := range p.SeqsAndRTTS {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	//File with maps for both sequence numbers and RTTs values
	file3, err3 := os.Create("Sequence_RTTs.txt")
	if err3 != nil {
		log.Fatal("Cannot create file", err3)
	}
	defer file3.Close()

	for _, k := range keys {
		fmt.Fprintf(file3, "%d\t %.2f\n", k, p.SeqsAndRTTS[k])
	}

	//fmt.Fprintf(file3, "%v", p.SeqsAndRTTS)
	//fmt.Printf("File has been created and ready to complete script")
	signal.Stop(c)
	p.Stop()
}

func validateSpacing(trainS int, trainI int, interval int, gamma int) bool {

	var validSpacing bool
	validSpacing = true

	if (trainS * trainI) >= interval-gamma {
		validSpacing = false
	}

	return validSpacing
}

func main() {
	var useUDP, debug bool
	var count, interval, trainS, trainI, gamma int
	var rate float64
	var source, hostname, pattern string
	var intPattern []int
	flag.BoolVar(&useUDP, "u", false, "use non-privileged datagram-oriented UDP as ICMP endpoints (shorthand)")
	flag.BoolVar(&debug, "d", false, "debug statements")
	flag.IntVar(&count, "c", 10, "number of probes to send")
	flag.IntVar(&interval, "i", 1000, "average for train interarrival (milliseconds)")
	flag.IntVar(&trainS, "t", 1, "number of pings in single train")
	flag.IntVar(&trainI, "I", 100, "interval in between pings in a train (milliseconds)")
	flag.IntVar(&gamma, "g", 0, "gamma for uniform distribution (milliseconds)")
	flag.Float64Var(&rate, "r", 0.0, "rate parameter, number of events, in average, present in a minute. Default 1 event each 100msec")
	flag.StringVar(&source, "s", "", "source address")
	flag.StringVar(&pattern, "p", "", "pattern of sizes to use in comma separated format (no spaces)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  %s [options] hostname [source]\n\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	hostname = flag.Arg(0)
	if len(hostname) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	if pattern != "" {
		//Process pattern
		vals := strings.Split(pattern, ",")
		for _, i := range vals {
			j, err := strconv.Atoi(i)
			if err != nil {
				flag.Usage()
				os.Exit(1)
			}
			intPattern = append(intPattern, j)
		}
	}

	ping(hostname, source, useUDP, debug, count, interval, trainS, trainI, gamma, rate, intPattern)

}

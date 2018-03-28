package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	mpping "github.com/wontoniii/go-mp-ping"
)

//Comment Line
type response struct {
	addr *net.IPAddr   //Pointer IP address
	rtt  time.Duration // RTT in seconds
	seqn int           // Sequence number
}

//Ping function in which we get the paramters to build the ICMP message.
//This function includes the basic paramaters to create the customer ICMP message we will be working with.
func ping(hostname, source string, useUDP, debug bool, count, interval, trainS, trainI, gamma int, pattern []int) {

	//We intialize the pinger
	p := mpping.NewPinger()

	//Set the network protocol to use, TCP by default, UDP if specified explicitly.
	if useUDP {
		p.Network("udp")
	}

	//Debug flag to be sert.
	if debug {
		p.Debug = true
	}

	//The Network protocol by default is IPv4
	//if the character ':' is identified, it means it is an IPv6 address.
	netProto := "ip4:icmp"
	if strings.Index(hostname, ":") != -1 {
		netProto = "ip6:ipv6-icmp"
	}

	//This methods resolves the IP address as the received string "hostname"
	//the network library returns it with the network format and the IP address.
	//ra will be the receiver address.
	//if we fail to setup the receiver address, we get an error.
	ra, err := net.ResolveIPAddr(netProto, hostname)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	//If source IP is not defined, then we set the default IP address for the system
	if source != "" {
		p.Source(source)
	}

	//If we are using more than one train, then we set the train interval to work with in Milliseconds.
	if trainS > 1 {
		p.Train = true       //Train usage flag
		p.TrainSize = trainS // Train size
		if trainI > 0 {
			p.TrainInt = time.Duration(trainI) * time.Millisecond //Train spacing interval is defined if TrainI parameter is greater than 0
		}
	}

	if gamma > 0 {
		p.Gamma = time.Duration(gamma) * time.Millisecond
	}

	//Here we define tha array of sizes to be used.
	//if it is greater than 0 then we set pattern with the values received.
	if len(pattern) > 0 {
		p.SetPattern(pattern)
	}

	//As we are only using one single IP address, we take the one we received in the "hostname" variable.
	p.AddIPAddr(ra)

	//We create two channels, two dynamically sized arrays.
	//OnRecv, for the responses received, each response will have, replier address, RTT and sequence number.

	//onIdle, we receive a boolen to know once the RTT has been exceeded.
	onRecv, onIdle := make(chan *response), make(chan bool)

	//When we get the event onRecv we add a response to the OnRecv channel.
	//The reponse will be composed by the IP address of the replier (as a pointer), the RTT and the sequence number.
	p.OnRecv = func(addr *net.IPAddr, t time.Duration, seqn int) {
		onRecv <- &response{addr: addr, rtt: t, seqn: seqn}
	}

	//Once the RTT has been execeded, the onIdle event is triggered and we set the flag to true.
	p.OnIdle = func() {
		onIdle <- true
	}

	//As we are implement RunLoop, we will use the MaxRTT as the interval between trains of probes.
	//i = interval between trains.
	//I = interval between pings within a train.
	p.MaxRTT = time.Duration(interval) * time.Millisecond

	//Sent keeps track of probes sent, received of the received ones and
	//crec is the flag to identify if we have entered the onRecv event.
	var sent, received int

	//var crec int
	var min, max, avg, mdev float32

	//We create a slice, dynamically array to hold the RTT value of the replies received.
	//The original lenght of the slice is 0
	results := make([]float32, 0)

	//We create a slice, dynamically array to hold the 'responses' obtained when we get a reply and trigger the onRecv event.
	//The key is the IP address we are pinging to in String format, the responses are stored in "response" pointer type.
	replies := make(map[string]*response)

	//As we are only using one IP address we set the key for the replies array to the single IP addres we are working with.
	//That IP address is held in the ra variable.
	replies[ra.String()] = nil

	//Printing initial message to format the output in the standard ping output.
	fmt.Printf("PING %s (%s) %d(%d) bytes of data\n", hostname, ra.String(), 0, 0)

	//Time stamp of the experiment time.
	st := time.Now()

	//We initialize the number of packet sent with the size of the train we have defined.
	//For example if we are going to send trains of 3 pings, the packets sents in the 1st iteration is 3.
	//sent = trainS

	sent = 0
	received = 0

	//We use the recursive option to send pings repeadetly
	p.RunLoop()

	//We create a channel to hold the signal received from the OS to interrupt or terminate the script execution.
	c := make(chan os.Signal, 1)

	//We add two values within it, the interrupt and the terminate.
	//These two values have been addded to the 'c' dynamic array created before.
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)

loop:
	for {
		if sent < count {
			select {
			case <-c:
				fmt.Println("get interrupted")
				break loop
			case res := <-onRecv:
				received++
				results = append(results, float32(res.rtt)/float32(time.Millisecond))
				if _, ok := replies[res.addr.String()]; ok {
					replies[res.addr.String()] = res
				}
			case <-onIdle:
				for host, r := range replies {
					if r == nil {
						fmt.Printf("%s : unreachable %v\n", host, time.Now())
						sent++
					} else {
						//fmt.Printf("%s : %v %v\n", host, r.rtt, time.Now())
						fmt.Printf("%d bytes from %s: icmp_seq=%d ttl=%d time=%.2f ms\n", p.Size+56, host, r.seqn+1, 128, float32(r.rtt)/float32(time.Millisecond))
						sent++
					}
					replies[host] = nil
				}
			case <-p.Done():
				if err = p.Err(); err != nil {
					fmt.Println("Ping failed:", err)
				}
				break loop
			}
		} else {
			break loop
		}
		//fmt.Printf("Sent Packages: %d.\n", sent)
	}

	//Computing statistics from pings received

	et := time.Now()
	elapsed := et.Sub(st)
	fmt.Printf("--- %s ping statistics ---\n", hostname)
	fmt.Printf("%d packets transmitted, %d received, %.2f%% packet loss, time %d ms\n", sent, received, float32(sent-received)/float32(sent), int(elapsed/time.Millisecond))

	lenResults := float32(len(results))
	fmt.Printf("Lenght of Results %v.\n", lenResults)
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

	signal.Stop(c)
	p.Stop()

}

func main() {
	var useUDP, debug bool
	var count, interval, trainS, trainI, gamma int
	var source, hostname, pattern string
	var intPattern []int
	flag.BoolVar(&useUDP, "u", false, "use non-privileged datagram-oriented UDP as ICMP endpoints (shorthand)")
	flag.BoolVar(&debug, "d", false, "debug statements")
	flag.IntVar(&count, "c", 10, "number of probes to send")
	flag.IntVar(&interval, "i", 1000, "average for probes interarrival (milliseconds)")
	flag.IntVar(&trainS, "t", 1, "number of pings in single train")
	flag.IntVar(&trainI, "I", 100, "interval in between probes in a train (milliseconds)")
	flag.IntVar(&gamma, "g", 0, "gamma for uniform distribution (milliseconds)")
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

	ping(hostname, source, useUDP, debug, count, interval, trainS, trainI, gamma, intPattern)

}

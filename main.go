package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"time"

	bw "github.com/immesys/bw2bind"
	"github.com/kidoman/embd"
	_ "github.com/kidoman/embd/host/rpi" // This loads the RPi driver
	"gopkg.in/yaml.v2"
)

//7,0,1,2,3,4,5,6
var relays []embd.DigitalPin
var BWC *bw.BW2Client
var PAC string

const tsFMT = "2006-01-02T15:04:05 MST"

type MetaTuple struct {
	Val string `yaml:"val"`
	TS  string `yaml:"ts"`
}

func (mt *MetaTuple) NewerThan(t time.Time) bool {
	mttime, err := time.Parse(tsFMT, mt.TS)
	if err != nil {
		fmt.Println("BAD METADATA TIME TAG: ", err)
		return false
	}
	return mttime.After(t)
}

type Plug struct {
	Base        string
	Meta        map[string]MetaTuple
	CommonNames []string `yaml:"common_names"`
}

var config struct {
	Meta           map[string]MetaTuple
	PermissionBase string
	Plugs          []Plug
}

func initHardware() {
	err := embd.InitGPIO()
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays = make([]embd.DigitalPin, 8)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[0], err = embd.NewDigitalPin(4) //7)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[1], err = embd.NewDigitalPin(17) //0)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[2], err = embd.NewDigitalPin(18) //1)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[3], err = embd.NewDigitalPin(27) //2)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[4], err = embd.NewDigitalPin(22) //3)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[5], err = embd.NewDigitalPin(23) //4)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[6], err = embd.NewDigitalPin(24) //5)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	relays[7], err = embd.NewDigitalPin(25) //6)
	if err != nil {
		fmt.Println("GPIO ERR:", err)
		os.Exit(1)
	}
	for _, r := range relays {
		err = r.SetDirection(embd.Out)
		if err != nil {
			fmt.Println("GPIO ERR:", err)
			os.Exit(1)
		}
		err = r.Write(embd.Low)
		if err != nil {
			fmt.Println("GPIO ERR:", err)
			os.Exit(1)
		}
	}
}

func initConfig() {
	contents, err := ioutil.ReadFile("/etc/powerup/config.yml")
	if err != nil {
		fmt.Println("Could not load config file. Aborting: ", err)
		os.Exit(1)
	}
	pass1, err := bw.LoadConfigFile(string(contents))
	if err != nil {
		fmt.Println("Could not load config file. Aborting: ", err)
		os.Exit(1)
	}
	fmt.Println("parsed: ", string(pass1))
	err = yaml.Unmarshal(pass1, &config)
	if err != nil {
		fmt.Println("Could not load config file. Aborting: ", err)
		os.Exit(1)
	}
}

func mergeMetadata() {
	doTuple := func(tgt string, mt MetaTuple) {
		mttime, err := time.Parse(tsFMT, mt.TS)
		if err != nil {
			fmt.Println("Metadata tag has bad timestamp:", tgt)
			return
		}
		ex_metadata, err := BWC.QueryOne(&bw.QueryParams{
			URI:                tgt,
			PrimaryAccessChain: PAC,
			ElaboratePAC:       bw.ElaborateFull,
		})
		if err != nil {
			fmt.Println("Could not query metadata: ", err)
			return
		}
		if ex_metadata != nil {
			entry, ok := ex_metadata.GetOnePODF(bw.PODFSMetadata).(bw.MsgPackPayloadObject)
			if ok {
				obj := bw.MetadataTuple{}
				entry.ValueInto(&obj)
				if !mt.NewerThan(obj.Time()) {
					fmt.Println("Existing metadata is same/newer for: ", tgt)
					return
				}
			}
		}
		po, err := bw.CreateMsgPackPayloadObject(bw.PONumSMetadata, &bw.MetadataTuple{
			Value:     mt.Val,
			Timestamp: mttime.UnixNano(),
		})
		if err != nil {
			fmt.Println("Could not create PO: ", err)
		}

		err = BWC.Publish(&bw.PublishParams{
			URI:                tgt,
			PrimaryAccessChain: PAC,
			ElaboratePAC:       bw.ElaborateFull,
			PayloadObjects:     []bw.PayloadObject{po},
			Persist:            true,
		})
		if err != nil {
			fmt.Println("Unable to update metadata: ", err)
		} else {
			fmt.Printf("set %s to %v @(%s)\n", tgt, mt.Val, mt.TS)
		}
	}

	for _, pl := range config.Plugs {
		for mkey, mt := range pl.Meta {
			tgt := config.PermissionBase + "/" + pl.Base + "/!meta/" + mkey
			doTuple(tgt, mt)
		}
		for mkey, mt := range config.Meta {
			tgt := config.PermissionBase + "/" + pl.Base + "/!meta/" + mkey
			doTuple(tgt, mt)
		}
		//DO THIS CORRECTLY AFTER THE DEMO
		if len(pl.CommonNames) > 0 {
			tgt := config.PermissionBase + "/" + pl.Base + "/binary/ctl/state/!common_names"
			po, err := bw.CreateMsgPackPayloadObject(bw.PONumSMetadata, &struct {
				Value     string
				Timestamp int64
				Extra     []string
				Type      string
			}{pl.CommonNames[0], time.Now().UnixNano(), pl.CommonNames[1:], "binary,actuator"})
			if err != nil {
				fmt.Println("Could not create PO: ", err)
			}
			err = BWC.Publish(&bw.PublishParams{
				URI:                tgt,
				PrimaryAccessChain: PAC,
				ElaboratePAC:       bw.ElaborateFull,
				PayloadObjects:     []bw.PayloadObject{po},
				Persist:            true,
			})
			if err != nil {
				fmt.Println("Unable to update common names: ", err)
			}
		}
	}
}

func main() {
	initConfig()
	var err error
	BWC, err = bw.Connect("localhost:28589")
	if err != nil {
		fmt.Println("Could not connect to local router: ", err)
		os.Exit(1)
	}
	us, err := BWC.SetEntityFile("entity.key")
	if err != nil {
		fmt.Println("Could not set entity key: ", err)
		os.Exit(1)
	}
	fmt.Println("entity set: ", us)
	uri := config.PermissionBase

	//Build a chain
	rc, err := BWC.BuildAnyChain(uri, "PC+", us)
	if err != nil {
		fmt.Println("Could not build permission chain: ", err)
		os.Exit(1)
	}
	PAC = rc.Hash
	mergeMetadata()
	initHardware()
	for idx := 0; idx < 7; idx++ {
		i := idx
		tgt := config.PermissionBase + "/" + config.Plugs[i].Base + "/binary/ctl/state"

		mc, err := BWC.Subscribe(&bw.SubscribeParams{
			URI:                tgt,
			PrimaryAccessChain: PAC,
			ElaboratePAC:       bw.ElaborateFull,
		})
		if err != nil {
			fmt.Println("Could not subscribe to ", tgt)
		}
		go func() {
			for m := range mc {
				fmt.Println("GOT MESSAGE on", tgt)
				m.Dump()
				po := m.GetOnePODF(bw.PODFBinaryActuation)
				if po != nil {
					if po.GetContents()[0] == 0 {
						fmt.Println("Would turn off:", i)
						relays[i].Write(0)
					} else if po.GetContents()[0] == 1 {
						fmt.Println("Would turn on:", i)
						relays[i].Write(1)
					}
				}
			}
		}()
	}
	for {
		runtime.Gosched()
	}
}

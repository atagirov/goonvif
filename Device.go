package goonvif

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/atagirov/goonvif/Device"
	"github.com/atagirov/goonvif/gosoap"
	"github.com/atagirov/goonvif/networking"
	"github.com/beevik/etree"
	WS_Discovery "github.com/yakovlevdmv/WS-Discovery"
)

var Xlmns = map[string]string{
	"onvif":   "http://www.onvif.org/ver10/schema",
	"tds":     "http://www.onvif.org/ver10/device/wsdl",
	"trt":     "http://www.onvif.org/ver10/media/wsdl",
	"tev":     "http://www.onvif.org/ver10/events/wsdl",
	"tptz":    "http://www.onvif.org/ver20/ptz/wsdl",
	"timg":    "http://www.onvif.org/ver20/imaging/wsdl",
	"tan":     "http://www.onvif.org/ver20/analytics/wsdl",
	"xmime":   "http://www.w3.org/2005/05/xmlmime",
	"wsnt":    "http://docs.oasis-open.org/wsn/b-2",
	"xop":     "http://www.w3.org/2004/08/xop/include",
	"wsa":     "http://www.w3.org/2005/08/addressing",
	"wstop":   "http://docs.oasis-open.org/wsn/t-1",
	"wsntw":   "http://docs.oasis-open.org/wsn/bw-2",
	"wsrf-rw": "http://docs.oasis-open.org/wsrf/rw-2",
	"wsaw":    "http://www.w3.org/2006/05/addressing/wsdl",
}

type DeviceType int

const (
	NVD DeviceType = iota
	NVS
	NVA
	NVT
)

func (devType DeviceType) String() string {
	stringRepresentation := []string{
		"NetworkVideoDisplay",
		"NetworkVideoStorage",
		"NetworkVideoAnalytics",
		"NetworkVideoTransmitter",
	}
	i := uint8(devType)
	switch {
	case i <= uint8(NVT):
		return stringRepresentation[i]
	default:
		return strconv.Itoa(int(i))
	}
}

//deviceInfo struct contains general information about ONVIF device
type deviceInfo struct {
	Manufacturer    string
	Model           string
	FirmwareVersion string
	SerialNumber    string
	HardwareId      string
}

//deviceInfo struct represents an abstract ONVIF device.
//It contains methods, which helps to communicate with ONVIF device
type device struct {
	xaddr    string
	login    string
	password string
	timeDiff time.Duration

	endpoints map[string]string
	info      deviceInfo
}

func (dev *device) GetServices() map[string]string {
	return dev.endpoints
}

func readResponse(resp *http.Response) string {
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func GetAvailableDevicesAtSpecificEthernetInterface(interfaceName string) []device {
	/*
		Call an WS-Discovery Probe Message to Discover NVT type Devices
	*/
	devices := WS_Discovery.SendProbe(interfaceName, nil, []string{"dn:" + NVT.String()}, map[string]string{"dn": "http://www.onvif.org/ver10/network/wsdl"})
	nvtDevices := make([]device, 0)
	////fmt.Println(devices)
	for _, j := range devices {
		doc := etree.NewDocument()
		if err := doc.ReadFromString(j); err != nil {
			fmt.Errorf("%s", err.Error())
			return nil
		}
		////fmt.Println(j)
		endpoints := doc.Root().FindElements("./Body/ProbeMatches/ProbeMatch/XAddrs")
		for _, xaddr := range endpoints {
			//fmt.Println(xaddr.Tag,strings.Split(strings.Split(xaddr.Text(), " ")[0], "/")[2] )
			xaddr := strings.Split(strings.Split(xaddr.Text(), " ")[0], "/")[2]
			fmt.Println(xaddr)
			c := 0
			for c = 0; c < len(nvtDevices); c++ {
				if nvtDevices[c].xaddr == xaddr {
					fmt.Println(nvtDevices[c].xaddr, "==", xaddr)
					break
				}
			}
			if c < len(nvtDevices) {
				continue
			}
			dev, err := NewDevice(strings.Split(xaddr, " ")[0])
			//fmt.Println(dev)
			if err != nil {
				fmt.Println("Error", xaddr)
				fmt.Println(err)
				continue
			} else {
				////fmt.Println(dev)
				nvtDevices = append(nvtDevices, *dev)
			}
		}
		////fmt.Println(j)
		//nvtDevices[i] = NewDevice()
	}
	return nvtDevices
}

func (dev *device) getSupportedServices(resp *http.Response) {
	//resp, err := dev.CallMethod(Device.GetCapabilities{Category:"All"})
	//if err != nil {
	//	log.Println(err.Error())
	//return
	//} else {
	doc := etree.NewDocument()

	data, _ := ioutil.ReadAll(resp.Body)

	if err := doc.ReadFromBytes(data); err != nil {
		//log.Println(err.Error())
		return
	}
	services := doc.FindElements("./Envelope/Body/GetCapabilitiesResponse/Capabilities/*/XAddr")
	for _, j := range services {
		////fmt.Println(j.Text())
		////fmt.Println(j.Parent().Tag)
		dev.addEndpoint(j.Parent().Tag, j.Text())
	}
	//}
}

type onvifError struct {
	Message string
	Inner   error
}

func newOnvifError(message string, inner error) *onvifError {
	return &onvifError{
		Message: message,
		Inner:   inner,
	}
}

func (onvifError *onvifError) Error() string {
	return onvifError.Message + ":" + onvifError.Inner.Error()
}

func NewDevice(xaddr string) (*device, error) {
	return NewDeviceWithUser(xaddr, "", "")
}

//NewDevice function construct a ONVIF Device entity
func NewDeviceWithUser(xaddr, login, password string) (*device, error) {
	dev := new(device)
	dev.xaddr = xaddr
	dev.endpoints = make(map[string]string)
	dev.addEndpoint("Device", "http://"+xaddr+"/onvif/device_service")

	getSystemDateAndTime := Device.GetSystemDateAndTime{}
	resp, err := dev.CallMethod(getSystemDateAndTime)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, newOnvifError("camera is not available at "+xaddr+" or it does not support ONVIF services", err)
	}
	dev.checkSystemDateAndTime(resp)

	if login != "" {
		dev.login = login
		dev.password = password
	}

	getCapabilities := Device.GetCapabilities{Category: "All"}

	resp, err = dev.CallMethod(getCapabilities)
	//fmt.Println(resp.Request.Host)
	//fmt.Println(readResponse(resp))
	if err != nil || resp.StatusCode != http.StatusOK {
		//panic(errors.New("camera is not available at " + xaddr + " or it does not support ONVIF services"))
		return nil, newOnvifError("camera is not available at "+xaddr+" or it does not support ONVIF services", err)
	}

	dev.getSupportedServices(resp)
	return dev, nil
}

func (dev *device) checkSystemDateAndTime(resp *http.Response) {
	currentTime := time.Now().UTC()

	doc := etree.NewDocument()
	data, _ := ioutil.ReadAll(resp.Body)
	if err := doc.ReadFromBytes(data); err != nil {
		return
	}
	utcDateTimeElement := doc.FindElement("./Envelope/Body/GetSystemDateAndTimeResponse/SystemDateAndTime/UTCDateTime")
	if utcDateTimeElement == nil {
		return
	}

	timeElement := utcDateTimeElement.FindElement("Time")
	dateElement := utcDateTimeElement.FindElement("Date")

	year, _ := strconv.Atoi(dateElement.SelectElement("Year").Text())
	month, _ := strconv.Atoi(dateElement.SelectElement("Month").Text())
	day, _ := strconv.Atoi(dateElement.SelectElement("Day").Text())
	hour, _ := strconv.Atoi(timeElement.SelectElement("Hour").Text())
	minute, _ := strconv.Atoi(timeElement.SelectElement("Minute").Text())
	second, _ := strconv.Atoi(timeElement.SelectElement("Second").Text())

	deviceTime := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)

	dev.timeDiff = deviceTime.Sub(currentTime)
}

func (dev *device) addEndpoint(Key, Value string) {
	dev.endpoints[Key] = Value
}

//Authenticate function authenticate client in the ONVIF Device.
//Function takes <username> and <password> params.
//You should use this function to allow authorized requests to the ONVIF Device
//To change auth data call this function again.
func (dev *device) Authenticate(username, password string) {
	dev.login = username
	dev.password = password
}

//GetEndpoint returns specific ONVIF service endpoint address
func (dev *device) GetEndpoint(name string) string {
	return dev.endpoints[name]
}

func buildMethodSOAP(msg string) (gosoap.SoapMessage, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(msg); err != nil {
		//log.Println("Got error")

		return "", err
	}
	element := doc.Root()

	soap := gosoap.NewEmptySOAP()
	soap.AddBodyContent(element)
	//soap.AddRootNamespace("onvif", "http://www.onvif.org/ver10/device/wsdl")

	return soap, nil
}

//CallMethod functions call an method, defined <method> struct.
//You should use Authenticate method to call authorized requests.
func (dev device) CallMethod(method interface{}) (*http.Response, error) {
	pkgPath := strings.Split(reflect.TypeOf(method).PkgPath(), "/")
	pkg := pkgPath[len(pkgPath)-1]

	var endpoint string
	switch pkg {
	case "Device":
		endpoint = dev.endpoints["Device"]
	case "Event":
		endpoint = dev.endpoints["Event"]
	case "Imaging":
		endpoint = dev.endpoints["Imaging"]
	case "Media":
		endpoint = dev.endpoints["Media"]
	case "PTZ":
		endpoint = dev.endpoints["PTZ"]
	}

	//TODO: Get endpoint automatically
	if dev.login != "" && dev.password != "" {
		return dev.callAuthorizedMethod(endpoint, method)
	} else {
		return dev.callNonAuthorizedMethod(endpoint, method)
	}
}

//CallNonAuthorizedMethod functions call an method, defined <method> struct without authentication data
func (dev device) callNonAuthorizedMethod(endpoint string, method interface{}) (*http.Response, error) {
	//TODO: Get endpoint automatically
	/*
		Converting <method> struct to xml string representation
	*/
	output, err := xml.MarshalIndent(method, "  ", "    ")
	if err != nil {
		//log.Printf("error: %v\n", err.Error())
		return nil, err
	}

	/*
		Build an SOAP request with <method>
	*/
	soap, err := buildMethodSOAP(string(output))
	if err != nil {
		//log.Printf("error: %v\n", err)
		return nil, err
	}

	/*
		Adding namespaces
	*/
	soap.AddRootNamespaces(Xlmns)

	/*
		Sending request and returns the response
	*/
	return networking.SendSoap(endpoint, soap.String())
}

//CallMethod functions call an method, defined <method> struct with authentication data
func (dev device) callAuthorizedMethod(endpoint string, method interface{}) (*http.Response, error) {
	/*
		Converting <method> struct to xml string representation
	*/
	output, err := xml.MarshalIndent(method, "  ", "    ")
	if err != nil {
		//log.Printf("error: %v\n", err.Error())
		return nil, err
	}

	/*
		Build an SOAP request with <method>
	*/
	soap, err := buildMethodSOAP(string(output))
	if err != nil {
		//log.Printf("error: %v\n", err.Error())
		return nil, err
	}

	/*
		Adding namespaces and WS-Security headers
	*/
	soap.AddRootNamespaces(Xlmns)

	soap.AddWSSecurity(dev.login, dev.password, dev.timeDiff)

	/*
		Sending request and returns the response
	*/
	return networking.SendSoap(endpoint, soap.String())
}

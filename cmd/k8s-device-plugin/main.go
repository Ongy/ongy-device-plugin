// Kubernetes (k8s) device plugin to enable registration of AMD GPU to a container cluster
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/kubevirt/device-plugin-manager/pkg/dpm"
	"golang.org/x/exp/slices"
	"golang.org/x/net/context"
	yaml "gopkg.in/yaml.v3"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type currentDevices struct {
	pluginName string
	devices    []string
}

type PluginDeviceConfig struct {
	HostPaths     []string `yaml:"hostPaths"`
	ContainerPath string   `yaml:"containerPath"`
}

type PluginConfig struct {
	Devices map[string]PluginDeviceConfig `yaml:"devices"`
}

// Plugin is identical to DevicePluginServer interface of device plugin API.
type Plugin struct {
	Device    PluginDeviceConfig
	Heartbeat chan []string
}

// Start is an optional interface that could be implemented by plugin.
// If case Start is implemented, it will be executed by Manager after
// plugin instantiation and before its registration to kubelet. This
// method could be used to prepare resources before they are offered
// to Kubernetes.
func (p *Plugin) Start() error {
	glog.Infof("Starting plugin: %v", p.Device.HostPaths)
	return nil
}

// Stop is an optional interface that could be implemented by plugin.
// If case Stop is implemented, it will be executed by Manager after the
// plugin is unregistered from kubelet. This method could be used to tear
// down resources.
func (p *Plugin) Stop() error {
	glog.Infof("Stopping plugin: %v", p.Device.HostPaths)
	return nil
}

// func simpleHealthCheck() bool {
// 	return true
// }

// GetDevicePluginOptions returns options to be communicated with Device
// Manager
func (p *Plugin) GetDevicePluginOptions(ctx context.Context, e *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

// PreStartContainer is expected to be called before each container start if indicated by plugin during registration phase.
// PreStartContainer allows kubelet to pass reinitialized devices to containers.
// PreStartContainer allows Device Plugin to run device specific operations on the Devices requested
func (p *Plugin) PreStartContainer(ctx context.Context, r *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// ListAndWatch returns a stream of List of Devices
// Whenever a Device state change or a Device disappears, ListAndWatch
// returns the new list
func (p *Plugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	//	devs := make([]*pluginapi.Device, len(p.Devices))

	// func() {
	//
	// 	i := 0
	// 	for id := range p.Devices {
	// 		dev := &pluginapi.Device{
	// 			ID:     id,
	// 			Health: pluginapi.Healthy,
	// 		}
	// 		devs[i] = dev
	// 		i++
	// 	}
	// }()

	// s.Send(&pluginapi.ListAndWatchResponse{Devices: devs})
	//	devices := []*pluginapi.Device{{
	//		ID:     p.Device.HostPath,
	//		Health: pluginapi.Healthy,
	//	}}
	glog.Infof("Reporting device with id %v", p.Device.HostPaths)
	//	s.Send(&pluginapi.ListAndWatchResponse{Devices: devices})

	for {
		select {
		case beat := <-p.Heartbeat:
			if len(beat) == 0 {
				s.Send(&pluginapi.ListAndWatchResponse{Devices: []*pluginapi.Device{}})
				glog.Infof("Not Exiting plugin (1): %s", p.Device.HostPaths)
				continue
				//return nil
			}

			devices := make([]*pluginapi.Device, len(beat))
			for i, device := range beat {
				devices[i] = &pluginapi.Device{
					ID:     device,
					Health: pluginapi.Healthy,
				}
			}
			s.Send(&pluginapi.ListAndWatchResponse{Devices: devices})
		}
	}
	// returning a value with this function will unregister the plugin from k8s
}

// GetPreferredAllocation returns a preferred set of devices to allocate
// from a list of available ones. The resulting preferred allocation is not
// guaranteed to be the allocation ultimately performed by the
// devicemanager. It is only designed to help the devicemanager make a more
// informed allocation decision when possible.
func (p *Plugin) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}

// Allocate is called during container creation so that the Device
// Plugin can run device specific operations and instruct Kubelet
// of the steps to make the Device available in the container
func (p *Plugin) Allocate(ctx context.Context, r *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	var response pluginapi.AllocateResponse
	var car pluginapi.ContainerAllocateResponse
	var dev *pluginapi.DeviceSpec

	for _, req := range r.ContainerRequests {
		car = pluginapi.ContainerAllocateResponse{}

		for _, id := range req.DevicesIDs {
			glog.Infof("Allocating device ID: %s", id)

			dev = new(pluginapi.DeviceSpec)
			dev.HostPath = id
			dev.ContainerPath = p.Device.ContainerPath
			dev.Permissions = "rw"
			car.Devices = append(car.Devices, dev)
		}

		response.ContainerResponses = append(response.ContainerResponses, &car)
	}

	return &response, nil
}

// Lister serves as an interface between imlementation and Manager machinery. User passes
// implementation of this interface to NewManager function. Manager will use it to obtain resource
// namespace, monitor available resources and instantate a new plugin for them.
type Lister struct {
	Config        PluginConfig
	ResUpdateChan chan []currentDevices
	Heartbeat     chan bool
	Plugins       map[string]*Plugin
}

// GetResourceNamespace must return namespace (vendor ID) of implemented Lister. e.g. for
// resources in format "color.example.com/<color>" that would be "color.example.com".
func (l *Lister) GetResourceNamespace() string {
	return "ongy.net"
}

// Discover notifies manager with a list of currently available resources in its namespace.
// e.g. if "color.example.com/red" and "color.example.com/blue" are available in the system,
// it would pass PluginNameList{"red", "blue"} to given channel. In case list of
// resources is static, it would use the channel only once and then return. In case the list is
// dynamic, it could block and pass a new list each times resources changed. If blocking is
// used, it should check whether the channel is closed, i.e. Discover should stop.
func (l *Lister) Discover(pluginListCh chan dpm.PluginNameList) {
	for {
		select {
		case newResourcesList := <-l.ResUpdateChan: // New resources found
			newResources := []string{}
			for _, resource := range newResourcesList {
				if val, ok := l.Plugins[resource.pluginName]; ok {
					val.Heartbeat <- resource.devices
				} else {
					newResources = append(newResources, resource.pluginName)
				}
			}

			for k, p := range l.Plugins {
				if !slices.ContainsFunc(newResourcesList, func(resource currentDevices) bool { return resource.pluginName == k }) {
					glog.Infof("Removing devices for '%s'", k)
					p.Heartbeat <- []string{}
					delete(l.Plugins, k)
				}
			}

			if len(newResources) > 0 {
				glog.Infof("Found devices for '%v'", newResources)
				pluginListCh <- newResources
			}
		case <-pluginListCh: // Stop message received
			// Stop resourceUpdateCh
			return
		}
	}
}

// NewPlugin instantiates a plugin implementation. It is given the last name of the resource,
// e.g. for resource name "color.example.com/red" that would be "red". It must return valid
// implementation of a PluginInterface.
func (l *Lister) NewPlugin(resourceLastName string) dpm.PluginInterface {
	glog.Infof("Allocating device for %s", resourceLastName)
	plugin := &Plugin{
		Device:    l.Config.Devices[resourceLastName],
		Heartbeat: make(chan []string),
	}

	l.Plugins[resourceLastName] = plugin
	return plugin
}

var gitDescribe string

func configOrDie(configPath string) PluginConfig {
	file, err := os.Open(configPath)
	if err != nil {
		glog.Fatalf("Failed to open config file '%s': %v", configPath, err)
	}
	defer file.Close()

	var config PluginConfig
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		glog.Fatalf("Failed to parse config file '%s': %v", configPath, err)
	}

	return config
}

func main() {
	versions := [...]string{
		"Zigbee serial device plugin for Kubernetes",
		fmt.Sprintf("%s version %s", os.Args[0], gitDescribe),
	}

	flag.Usage = func() {
		for _, v := range versions {
			fmt.Fprintf(os.Stderr, "%s\n", v)
		}
		fmt.Fprintln(os.Stderr, "Usage:")
		flag.PrintDefaults()
	}
	var pulse int
	var configPath string
	flag.IntVar(&pulse, "pulse", 1, "time between health check polling in seconds.  Set to 0 to disable.")
	flag.StringVar(&configPath, "config-path", "/config/config.yaml", "Path to the config file.")
	// this is also needed to enable glog usage in dpm
	flag.Parse()

	for _, v := range versions {
		glog.Infof("%s", v)
	}

	l := Lister{
		ResUpdateChan: make(chan []currentDevices),
		Heartbeat:     make(chan bool),
		Config:        configOrDie(configPath),
		Plugins:       make(map[string]*Plugin),
	}
	manager := dpm.NewManager(&l)

	if pulse > 0 {
		go func() {
			glog.Infof("Heart beating every %d seconds", pulse)
			for {
				time.Sleep(time.Second * time.Duration(pulse))
				l.Heartbeat <- true
			}
		}()
	}

	go func() {
		for {
			select {
			case <-l.Heartbeat:
				lastNames := []currentDevices{}
				for lastName, device := range l.Config.Devices {
					found := []string{}
					for _, hostpath := range device.HostPaths {
						if _, err := os.Stat(hostpath); errors.Is(err, os.ErrNotExist) {
							//glog.Infof("Didn't find device for '%s'", lastName)
							continue
						}
						found = append(found, hostpath)
					}

					if len(found) > 0 {
						lastNames = append(lastNames, currentDevices{pluginName: lastName, devices: found})
					}
				}

				l.ResUpdateChan <- lastNames
			}
		}
	}()
	l.Heartbeat <- true

	manager.Run()
}

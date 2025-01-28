package mdns

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"

	"github.com/celebdor/zeroconf"
	"github.com/coredns/caddy"
)

func init() {
	caddy.RegisterPlugin("mdns", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})
}

func setup(c *caddy.Controller) error {
	c.Next()
	c.NextArg()
	domain := c.Val()
	mdnsTypes := []string{}
	minSRV := 3
	// Note that a filter of "" will match everything
	filter := ""
	bindAddress := ""

	for c.NextBlock() {
		switch c.Val() {
		case "type":
			remaining := c.RemainingArgs()
			if len(remaining) < 1 {
				return c.Errf("type needs to exist")
			}
			mdnsTypes = remaining
		case "min_srv_records":
			remaining := c.RemainingArgs()
			if len(remaining) != 1 {
				return c.Errf("min_srv_records needs a number")
			}
			srvInt, err := strconv.Atoi(remaining[0])
			if err != nil {
				return c.Errf("min_srv_records provided is invalid")
			}
			minSRV = srvInt
		case "filter_text":
			remaining := c.RemainingArgs()
			if len(remaining) != 1 {
				return c.Errf("filter needs text to filter")
			}
			filter = remaining[0]
		case "bind_address":
			remaining := c.RemainingArgs()
			if len(remaining) != 1 {
				return c.Errf("bind_address needs an address to bind to")
			}
			bindAddress = remaining[0]
		default:
			return c.Errf("unknown property '%s'", c.Val())
		}
	}

	if len(mdnsTypes) == 0 {
		mdnsTypes = []string{"_workstation._tcp"}
	}

	log.Infof("domain:          %s", domain)
	log.Infof("type:            %v", mdnsTypes)
	log.Infof("min_srv_records: %d", minSRV)
	log.Infof("filter_text:     %s", filter)
	log.Infof("bind_address:    %s", bindAddress)

	// Because the plugin interface uses a value receiver, we need to make these
	// pointers so all copies of the plugin point at the same maps.
	mdnsHosts := make(map[string]*zeroconf.ServiceEntry)
	srvHosts := make(map[string][]*zeroconf.ServiceEntry)
	cnames := make(map[string]string)
	mutex := sync.RWMutex{}
	m := MDNS{
		Domain:      strings.TrimSuffix(domain, "."),
		mdnsTypes:   mdnsTypes,
		minSRV:      minSRV,
		filter:      filter,
		bindAddress: bindAddress,
		mutex:       &mutex,
		mdnsHosts:   &mdnsHosts,
		srvHosts:    &srvHosts,
		cnames:      &cnames,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.OnStartup(func() error {
		go browseLoop(ctx, &m)
		return nil
	})
	c.OnShutdown(func() error {
		cancel()
		return nil
	})

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		m.Next = next
		return m
	})

	return nil
}

func browseLoop(ctx context.Context, m *MDNS) {
	for {
		m.BrowseMDNS(ctx)
		// 5 seconds seems to be the minimum ttl that the cache plugin will allow
		// Since each browse operation takes around 2 seconds, this should be fine
		time.Sleep(5 * time.Second)
	}
}

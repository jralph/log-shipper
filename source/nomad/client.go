package nomad

import (
	"fmt"
	consulAPI "github.com/hashicorp/consul/api"
	nomadAPI "github.com/hashicorp/nomad/api"
	"time"
)

type Client struct {
	Config map[string]string

	NomadClient *nomadAPI.Client
	ConsulClient *consulAPI.Client

	sessionID string
	sessionTTL string
}

func NewClient(config map[string]string) (*Client, error) {
	consulClient, err := NewConsulClient(config["consulAddr"])
	if err != nil {
		return nil, err
	}

	nomadClient, err := NewNomadClient(config["nomadAddr"])
	if err != nil {
		return nil, err
	}

	return &Client{
		Config: config,

		NomadClient: nomadClient,
		ConsulClient: consulClient,

		sessionTTL: "10s",
	}, nil
}

func NewConsulClient(consulAddr string) (*consulAPI.Client, error) {
	config := consulAPI.DefaultConfig()
	if len(consulAddr) != 0 {
		config.Address = consulAddr
	}

	return consulAPI.NewClient(config)
}

func NewNomadClient(nomadAddr string) (*nomadAPI.Client, error) {
	config := nomadAPI.DefaultConfig()
	if len(nomadAddr) != 0 {
		config.Address = nomadAddr
	}

	return nomadAPI.NewClient(config)
}

func (c *Client) ReceiveLogs(receiver chan<- []byte) {
	err := c.createSession()
	if err != nil {
		panic(err)
	}

	stop := make(chan struct{})
	defer close(stop)

	err = c.ConsulClient.Session().RenewPeriodic(c.sessionTTL, c.sessionID, nil, stop)
	if err != nil {
		panic(err)
	}

	allocationPool := NewAllocationPool()

	go c.syncAllocations(allocationPool)

	for {
		select {
		case alloc := <-allocationPool.AllocationAdded:

		case alloc := <-allocationPool.AllocationRemoved:

		}
	}
}

func (c *Client) syncAllocations(pool *AllocationPool) {
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		pool.Sync(c.getAllocations())
	}
}

func (c *Client) getAllocations() []*nomadAPI.Allocation {
	if len(c.Config["node"]) == 0 {
		var allocations []*nomadAPI.Allocation

		nodes, _, err := c.NomadClient.Nodes().List(nil)
		if err != nil {
			panic(err)
		}

		for _, node := range nodes {
			nodeAllocations, _, err := c.NomadClient.Nodes().Allocations(node.ID, nil)
			if err != nil {
				panic(err)
			}

			allocations = append(allocations, nodeAllocations...)
		}

		return allocations
	}

	nodesToSync := []string{c.Config["node"]}

	failover := false
	if c.Config["failover"] == "yes" {
		failover = true
	}

	currentAllocLock := &consulAPI.KVPair{
		Key: fmt.Sprintf("log-shipper/locks/nodes/%s/leader", c.Config["node"]),
		Value: []byte(fmt.Sprintf("primary node: %s", c.Config["node"])),
		Session: c.sessionID,
	}

	acquired, _, err := c.ConsulClient.KV().Acquire(currentAllocLock, nil)
	if err != nil {
		panic(err)
	}

	for !acquired {
		acquired, _, err = c.ConsulClient.KV().Acquire(currentAllocLock, nil)
		if err != nil {
			panic(err)
		}

		if !acquired {
			time.Sleep(1 * time.Second)
		}
	}

	if failover {
		otherNodes, _, err := c.NomadClient.Nodes().List(nil)
		if err != nil {
			panic(err)
		}

		for _, node := range otherNodes {
			lock := &consulAPI.KVPair{
				Key: fmt.Sprintf("log-shipper/locks/nodes/%s/leader", node.ID),
				Value: []byte(fmt.Sprintf("primary node: %s", c.Config["node"])),
				Session: c.sessionID,
			}

			acquired, _, _ := c.ConsulClient.KV().Acquire(lock, nil)

			failoverLock := &consulAPI.KVPair{
				Key: fmt.Sprintf("log-shipper/locks/nodes/%s/failover", node.ID),
				Value: []byte(fmt.Sprintf("primary node: %s", c.Config["node"])),
				Session: c.sessionID,
			}

			if acquired {
				acquiredFailover, _, _ := c.ConsulClient.KV().Acquire(failoverLock, nil)

				if acquiredFailover {
					nodesToSync = append(nodesToSync, node.ID)
				}

				_, _, _ = c.ConsulClient.KV().Release(lock, nil)
			} else {
				_, _, _ = c.ConsulClient.KV().Release(failoverLock, nil)
			}
		}
	}

	var allocations []*nomadAPI.Allocation

	for _, nodeID := range nodesToSync {
		nodeAllocations, _, err := c.NomadClient.Nodes().Allocations(nodeID, nil)
		if err != nil {
			panic(err)
		}

		allocations = append(allocations, nodeAllocations...)
	}

	return allocations
}

func (c *Client) createSession() error {
	session := &consulAPI.SessionEntry{
		TTL: c.sessionTTL,
		Behavior: "delete",
	}

	sessionID, _, err := c.ConsulClient.Session().Create(session, nil)
	if err != nil {
		return err
	}

	c.sessionID = sessionID
}
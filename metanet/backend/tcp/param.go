package tcp

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/crossmesh/fabric/common"
	"github.com/crossmesh/fabric/metanet/backend"
)

type parameters struct {
	autoBind bool `json:"auto_bind" default:"false"`

	Bind      string `json:"ep"`
	StartCode string `json:"start_code"`

	SendTimeout     uint32 `json:"send_timeout" default:"50"`
	SendBufferSize  int    `json:"send_buffer" default:"0"`
	KeepalivePeriod int    `json:"keepalive" default:"60"`
	ConnectTimeout  uint32 `json:"connect_timeout" default:"15"`
	MaxConcurrency  uint   `json:"max_concurrency" default:"1"`

	Encryption struct {
		// pre-shared key.
		PSK    string `json:"psk"`
		Enable bool   `json:"enable" default:"false"`
	} `json:"encrypt"`

	// drain options.
	Drainer struct {
		EnableDrainer        bool   `json:"enable" default:"false"`
		MaxDrainBuffer       uint32 `json:"max_buffer" default:"8388608"`     // maximum drain buffer in byte.
		MaxDrainLatancy      uint32 `json:"max_latency" default:"5"`          // maximum latency tolerance in microsecond.
		DrainStatisticWindow uint32 `json:"window" default:"1000"`            // statistic window in millisecond
		BulkThreshold        uint32 `json:"bulk_threshold" default:"2097152"` // rate threshold (Bps) to trigger bulk mode.
	} `json:"drainer"`
}

func (p *parameters) Clone() *parameters {
	new := &parameters{}
	*new = *p
	return new
}

func (p *parameters) Assign(x *parameters) { *p = *x }

func (p *parameters) Unmarshal(x []byte) error {
	if len(x) < 1 {
		x = []byte("{}")
	}
	return json.Unmarshal(x, p)
}

func (p *parameters) ResetDefault() {
	p.autoBind = false
	p.StartCode = ""
	p.SendTimeout = 50
	p.SendBufferSize = 0
	p.KeepalivePeriod = 60
	p.ConnectTimeout = 15
	p.MaxConcurrency = 1

	p.Encryption.Enable = false
	p.Encryption.PSK = ""

	p.Drainer.EnableDrainer = false
	p.Drainer.MaxDrainBuffer = 8388608
	p.Drainer.MaxDrainLatancy = 5
	p.Drainer.DrainStatisticWindow = 1000
	p.Drainer.BulkThreshold = 2097152
}

func (p *parameters) Marshal() ([]byte, error) { return json.Marshal(p) }

// SetParams sets parameters for endpoint.
func (m *BackendManager) SetParams(ep string, args []string) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	tx, err := m.resources.StoreTxn(true)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil {
				panic(rerr)
			}
		}
	}()

	endpoint, isNew, err := m.getEndpointOrCreate(tx, ep, true)
	if err != nil {
		return err
	}

	newParams := endpoint.parameters.Clone()

	paramList := common.ParamList{
		Item: []*common.ParamItem{
			{
				Name: "start_code",
				Args: []*common.ParamArgument{
					{
						Name:        "start code hex",
						Destination: &newParams.StartCode,
						Validator: common.StringParamValidation(func(startCode string) error {
							if len(startCode) > 0 {
								lead := make([]byte, hex.DecodedLen(len(startCode)))
								if _, err := hex.Decode(lead, []byte(startCode)); err != nil {
									return &common.ParamError{
										Msg: fmt.Sprintf("invalid start code \"%v\". (decode err = \"%v\")", startCode, err),
									}
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "send_timeout",
				Args: []*common.ParamArgument{
					{
						Name:        "timeout duration (ms)",
						Destination: &newParams.SendTimeout,
						Validator: common.Uint32ParamValidation(func(x uint32) error {
							if x < 1 {
								return &common.ParamError{
									Msg: "timeout duration is too short.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "keepalive",
				Args: []*common.ParamArgument{
					{
						Name:        "keepalive period (s)",
						Destination: &newParams.KeepalivePeriod,
						Validator: common.IntParamValidation(func(x int) error {
							if x < 1 {
								return &common.ParamError{
									Msg: "keepalive period is too short.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "connect_timeout",
				Args: []*common.ParamArgument{
					{
						Name:        "timeout (s)",
						Destination: &newParams.ConnectTimeout,
						Validator: common.Uint32ParamValidation(func(x uint32) error {
							if x < 1 {
								return &common.ParamError{
									Msg: "timeout is too short.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "send_buffer",
				Args: []*common.ParamArgument{
					{
						Name:        "size (bytes)",
						Destination: &newParams.SendBufferSize,
					},
				},
			},
			{
				Name: "max_concurrency",
				Args: []*common.ParamArgument{
					{
						Name:        "num of goroutines",
						Destination: &newParams.MaxConcurrency,
						Validator: common.UintParamValidation(func(x uint) error {
							return nil
						}),
					},
				},
			},
			{
				Name: "psk",
				Args: []*common.ParamArgument{
					{
						Name:        "key",
						Destination: &newParams.Encryption.PSK,
						Validator: common.StringParamValidation(func(psk string) error {
							if len(psk) < 6 && len(psk) > 0 {
								return &common.ParamError{
									Msg: "PSK is too short.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "max_drain_buffer",
				Args: []*common.ParamArgument{
					{
						Name:        "size (bytes)",
						Destination: &newParams.Drainer.MaxDrainBuffer,
						Validator: common.Uint32ParamValidation(func(size uint32) error {
							if size < 128 {
								return &common.ParamError{
									Msg: "buffer size is too small. 128 bytes is needed at least.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "max_drain_latency",
				Args: []*common.ParamArgument{
					{
						Name:        "duration (ms)",
						Destination: &newParams.Drainer.MaxDrainLatancy,
						Validator: common.Uint32ParamValidation(func(latency uint32) error {
							if latency < 1 {
								return &common.ParamError{
									Msg: "latency is too small. 1 ms is needed at least.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "drain_stat_win",
				Args: []*common.ParamArgument{
					{
						Name:        "window (ms)",
						Destination: &newParams.Drainer.DrainStatisticWindow,
						Validator: common.Uint32ParamValidation(func(latency uint32) error {
							if latency < 10 {
								return &common.ParamError{
									Msg: "stat win is too small. 10 ms is needed at least.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "bulk_thre",
				Args: []*common.ParamArgument{
					{
						Name:        "threshold (bytes)",
						Destination: &newParams.Drainer.DrainStatisticWindow,
						Validator: common.Uint32ParamValidation(func(latency uint32) error {
							if latency < 4096 {
								return &common.ParamError{
									Msg: "threshold is too small. 4096 is needed at least.",
								}
							}
							return nil
						}),
					},
				},
			},
			{
				Name: "encrypt",
				Args: []*common.ParamArgument{
					{
						Name:        "enabled (bool)",
						Destination: &newParams.Encryption.Enable,
						Validator:   common.BoolParamValidation(nil),
					},
				},
			},
			{
				Name: "drainer",
				Args: []*common.ParamArgument{
					{
						Name:        "enabled (bool)",
						Destination: &newParams.Drainer.EnableDrainer,
						Validator:   common.BoolParamValidation(nil),
					},
				},
			},
		},
	}

	var changedCount uint
	if _, changedCount, err = paramList.ParseArgs(args); err != nil {
		if _, isParamErr := err.(*common.ParamError); !isParamErr {
			err = &common.ParamError{
				Msg: err.Error(),
			}
		}
		return err
	}

	if changedCount < 0 && isNew {
		// nothing changed.
		return nil
	}

	var data []byte
	if data, err = newParams.Marshal(); err != nil {
		return err
	}

	var basePath []string
	basePath = append(basePath, tcpBackendNetworkPath...)
	path := append(basePath, ep)
	if err = tx.Set(path, data); err != nil {
		return err
	}

	tx.OnCommit(func() {
		endpoint.parameters = *newParams
	})
	return tx.Commit()
}

// ShowParams builds parameter map for endpoint.
func (m *BackendManager) ShowParams(eps ...string) ([]map[string]string, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	params := make([]map[string]string, 0)
	for i := 0; i < len(eps); i++ {
		ep := eps[i]
		endpoint, err := m.getEndpoint(ep)
		if err != nil {
			if _, isNotFound := err.(*backend.EndpointNotFoundError); !isNotFound {
				return nil, err
			}
		}
		if endpoint == nil {
			params = append(params, map[string]string(nil))
			continue
		}
		paramMap := make(map[string]string, 14)

		paramMap["bind"] = endpoint.parameters.Bind
		paramMap["start_code"] = endpoint.parameters.StartCode
		paramMap["send_timeout"] = strconv.FormatUint(uint64(endpoint.parameters.SendTimeout), 10)
		paramMap["send_buffer"] = strconv.FormatInt(int64(endpoint.parameters.SendBufferSize), 10)
		paramMap["keepalive"] = strconv.FormatInt(int64(endpoint.parameters.KeepalivePeriod), 10)
		paramMap["connect_timeout"] = strconv.FormatUint(uint64(endpoint.parameters.ConnectTimeout), 10)
		paramMap["max_concurrency"] = strconv.FormatUint(uint64(endpoint.parameters.MaxConcurrency), 10)
		if psk := endpoint.parameters.Encryption.PSK; len(psk) > 0 {
			paramMap["psk"] = "<secret>"
		} else {
			paramMap["psk"] = "<none>"
		}
		paramMap["encrypt"] = strconv.FormatBool(endpoint.parameters.Encryption.Enable)
		paramMap["drainer"] = strconv.FormatBool(endpoint.parameters.Drainer.EnableDrainer)
		paramMap["max_drain_buffer"] = strconv.FormatUint(uint64(endpoint.parameters.Drainer.MaxDrainBuffer), 10)
		paramMap["max_drain_latency"] = strconv.FormatUint(uint64(endpoint.parameters.Drainer.MaxDrainBuffer), 10)
		paramMap["drain_stat_win"] = strconv.FormatUint(uint64(endpoint.parameters.Drainer.DrainStatisticWindow), 10)
		paramMap["drain_bulk_thre"] = strconv.FormatUint(uint64(endpoint.parameters.Drainer.BulkThreshold), 10)

		params = append(params, paramMap)
	}

	return params, nil
}

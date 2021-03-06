package main

import (
	"encoding/json"
	"flag"
	"fmt"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/k-sone/snmpgo"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"time"
)

func SnmpWalk(ip string, community string, targetOid string) []byte {
	snmp, err := snmpgo.NewSNMP(snmpgo.SNMPArguments{
		Version:   snmpgo.V2c,
		Address:   ip + ":161",
		Retries:   1,
		Timeout:   time.Second * 1,
		Community: community,
	})
	if err != nil {
		fmt.Println(err)
		return []byte("{\"code\":-1}")
	}

	oids, err := snmpgo.NewOids([]string{targetOid})
	if err != nil {
		fmt.Println(err)
		return []byte("{\"code\":-1}")
	}

	if err = snmp.Open(); err != nil {
		fmt.Println(err)
		return []byte("{\"code\":-1}")
	}
	defer snmp.Close()

	pdu, err := snmp.GetBulkWalk(oids, 0, 10)
	if err != nil {
		fmt.Println(err)
		return []byte("{\"code\":-1}")
	}
	if pdu.ErrorStatus() != snmpgo.NoError {
		fmt.Println(pdu.ErrorStatus(), pdu.ErrorIndex())
	}

	datas := make(map[string]interface{})
	datas["code"] = 0
	datas["data"] = make(map[string]string)
	for _, v := range pdu.VarBinds() {
		key := v.Oid.String()[len(targetOid)+1:]
		datas["data"].(map[string]string)[key] = v.Variable.String()
	}
	json_datas, _ := json.Marshal(datas)

	return json_datas
}

func ApiSnmpWalk(w http.ResponseWriter, r *http.Request) {
	//读取内容
	body, _ := ioutil.ReadAll(r.Body)
	//json转map
	var req map[string]string
	err := json.Unmarshal(body, &req)
	if err != nil {
		fmt.Println(err)
		return
	}
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.Write(SnmpWalk(req["ip"], req["community"], req["targetOid"]))
}

func OnlineCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "online")
}

func registerServer() {

	fmt.Printf("注册到服务中心：%s，本服务地址：%s\n", consul_server_addr, consul_service_addr)

	config := consulapi.DefaultConfig()
	config.Address = consul_server_addr
	client, err := consulapi.NewClient(config)
	if err != nil {
		fmt.Println("consul client error : ", err)
	}

	registration := new(consulapi.AgentServiceRegistration)
	registration.ID = fmt.Sprintf("%s-%s", consul_service_name, consul_service_addr)
	registration.Name = consul_service_name
	registration.Port = listen_port
	registration.Address = consul_service_addr
	registration.Check = &consulapi.AgentServiceCheck{
		HTTP:                           fmt.Sprintf("http://%s:%d/online_check", consul_service_addr, listen_port),
		Timeout:                        "3s",
		Interval:                       "5s",
		DeregisterCriticalServiceAfter: "30s", //check失败后30秒删除本服务
	}

	err = client.Agent().ServiceRegister(registration)
	if err != nil {
		fmt.Println("register server error : ", err)
	}

	http.HandleFunc("/online_check", OnlineCheck)

}

func GetIntranetIp() []string {
	var ip []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip = append(ip, ipnet.IP.String())
			}
		}
	}
	return ip
}

var (
	is_reg_consul       bool
	listen_addr         string
	listen_port         int
	consul_service_addr string
	consul_server_addr  string
	consul_service_name string
)

func main() {
	//得到本地默认ip
	ip := GetIntranetIp()
	default_listen_addr := ip[len(ip)-1]
	//解析参数
	flag.StringVar(&listen_addr, "l", "0.0.0.0", "监听地址，如0.0.0.0")
	flag.IntVar(&listen_port, "p", 8085, "监听端口，如8085")
	flag.BoolVar(&is_reg_consul, "r", false, "是否注册到Consul")
	flag.StringVar(&consul_service_addr, "a", default_listen_addr, "Consul注册监听地址，如172.18.0.2，禁止使用0.0.0.0")
	flag.StringVar(&consul_server_addr, "s", "172.18.0.3:8500", "Consul服务器（注册）地址，如172.18.0.3:8500")
	flag.StringVar(&consul_service_name, "n", "snmpwalk-server", "Consul服务名称，如snmpwalk-server")
	flag.Parse()
	//启动服务
	http.HandleFunc("/api/snmpwalk", ApiSnmpWalk)
	fmt.Printf("服务器已启动：%s:%d/api/snmpwalk\n", listen_addr, listen_port)
	if is_reg_consul {
		go registerServer()
	}
	http.ListenAndServe(fmt.Sprintf("%s:%d", listen_addr, listen_port), nil)
}

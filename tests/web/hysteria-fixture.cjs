module.exports = {
  listen_port:443,obfs:{type:"off",password:"",minPacketSize:512,maxPacketSize:1200},
  masquerade:{type:"off",url:"",rewriteHost:true,insecure:false,xForwarded:false,dir:"",content:"",headers:{},statusCode:200,listenHTTP:"",listenHTTPS:"",forceHTTPS:false},
  quic:{initStreamReceiveWindow:8388608,maxStreamReceiveWindow:8388608,initConnReceiveWindow:20971520,maxConnReceiveWindow:20971520,maxIdleTimeout:"30s",maxIncomingStreams:1024,disablePathMTUDiscovery:false,disableStatelessReset:false},
  bandwidth:{up:"0",down:"0",disableLossCompensation:false},ignoreClientBandwidth:false,congestion:{type:"bbr",bbrProfile:"standard"},disableUDP:false,udpIdleTimeout:"60s",speedTest:false,
  resolver:{type:"system",addr:"",timeout:"10s",sni:"",insecure:false},sniff:{enable:false,timeout:"2s",rewriteDomain:false,tcpPorts:"all",udpPorts:"all"},
  protect_internal:true,blocked_countries:[],acl_rules:[],outbounds:[{name:"direct",type:"direct",mode:"auto",bindIPv4:"",bindIPv6:"",bindDevice:"",fastOpen:false,addr:"",username:"",password:"",url:"",insecure:false}],
};

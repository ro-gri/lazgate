const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const context = {window: {}, URL};
vm.createContext(context);
vm.runInContext(fs.readFileSync('internal/server/web/admin/ipaddr.js', 'utf8'), context);
context.window.ipaddr = context.ipaddr;
vm.runInContext(fs.readFileSync('internal/server/web/admin/amnezia-validation.js', 'utf8'), context);
const {validate, cpsError} = context.window.LazAmneziaValidation;
const base = {endpoint_host:'vpn.example.com', endpoint_port:'51820', dns:'1.1.1.1, 2606:4700:4700::1111', jc:'5',jmin:'48',jmax:'272',s1:'124',s2:'16',s3:'23',s4:'20',h1:'1',h2:'2',h3:'3',h4:'4',server_ipv4:'10.8.1.1',prefix:'24',mtu:'1280',persistent_keepalive:'25',route_mode:'all',ipv6_mode:'off'};
test('valid defaults, ranges and generated-style CPS',()=>{
 assert.equal(Object.keys(validate(base,true)).length,0);
 assert.equal(Object.keys(validate({...base,h1:'100-200',content_padding_addition:'32-128',RekeyTimeout:'2-5',i1:'<r 2><b 0x0100><r 8><t>'},true)).length,0);
});
test('invalid parameters and cross-field constraints',()=>{
 for(const [field,value] of Object.entries({endpoint_host:'https://example.com',endpoint_port:'65536',dns:'1.2.3.999',jc:'1.2',jmax:'20',s2:'11',h1:'2-4',h3:'4294967296',content_padding_addition:'50-2',RekeyTimeout:'-1',server_ipv4:'10.8.1.0',prefix:'31',mtu:'67',persistent_keepalive:'65536',i1:'<b 0x123>'})) assert.ok(validate({...base,[field]:value},true)[field],field);
 assert.ok(validate({...base,route_mode:'include',route_networks:'::/0'}).route_networks);
 assert.ok(validate({...base,ipv6_mode:'include',ipv6_networks:'10.0.0.0/8'}).ipv6_networks);
 assert.ok(validate({...base,cps_profile:'dns',cps_service:'custom',cps_domain:'127.0.0.1'}).cps_domain);
 assert.ok(validate({...base,server_ipv4:'10.8.1.255'}).server_ipv4);
});
test('CPS syntax and bounded expanded size',()=>{
 for(const s of ['<r 0>','<r 65508>','garbage','<b 0xgg>','<t>trailing','<rx 5>'])assert.ok(cpsError(s),s);
 for(const s of ['', '<b 0x00><rc 10><rd 2><t>', '<r 65507>'])assert.equal(cpsError(s),'',s);
});

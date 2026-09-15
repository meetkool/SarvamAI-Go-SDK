const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname, 'web/index.html'), 'utf8');
const elements = new Map();
function element() { return {textContent:'', className:'', appendChild(){}, remove(){}, classList:{add(){},remove(){}}}; }
const sandbox = {document:{getElementById(id){if(!elements.has(id)) elements.set(id,element());return elements.get(id);},createElement:element}, Float32Array,Int16Array,setTimeout,clearTimeout};
vm.createContext(sandbox);
vm.runInContext(html.match(/<script>([\s\S]*?)<\/script>/)[1],sandbox);
vm.runInContext(`
let created = 0, resumed = 0;
const ctx = {
 sampleRate:24000,currentTime:0,state:'running',destination:{},
 createBuffer(c,n,r){return {duration:n/r,getChannelData(){return new Float32Array(n);}};},
 createBufferSource(){created++;return {connect(){},start(){},stop(){},disconnect(){}};},
 createBiquadFilter(){return {frequency:{},Q:{},connect(){},disconnect(){}};},
 createGain(){return {gain:{value:0,linearRampToValueAtTime(){},cancelScheduledValues(){},setValueAtTime(){}},connect(){},disconnect(){}};},
 resume(){resumed++;this.state='running';return Promise.resolve();}
};
playCtx = ctx;
startAmbience();
const bed = ambience;
for(const state of ['thinking','speaking','listening','thinking','speaking']) handle({type:'state',state});
if (ambience !== bed || created !== 1) throw Error('Ambience restarted or tapping was added');
handle({type:'interrupt'});
if(ambience !== bed) throw Error('Interrupt stopped ambience');
ctx.state = 'suspended';
playPCM(new Int16Array(2400).buffer);
if(resumed !== 1 || sources.length !== 1) throw Error('Suspended playback did not recover');
handle({type:'interrupt'});
if(sources.length || playHead !== 0) throw Error('Interrupt did not clear speech');
`,sandbox);
console.log('Passed: continuous ambience, no typing bursts, playback resume, interrupt cleanup.');

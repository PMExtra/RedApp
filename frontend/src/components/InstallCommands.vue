<script setup lang="ts">
import { computed, ref } from 'vue'
const props=defineProps<{origin:string}>()
const commands=computed(()=>[{name:'Shell',command:`curl -fsSL '${props.origin}/install.sh' | sh`},{name:'PowerShell',command:`irm '${props.origin}/install.ps1' | iex`}])
const message=ref('')
async function copy(command:string){try{if(!navigator.clipboard)throw Error('Clipboard access requires HTTPS or localhost; select and copy the command manually');await navigator.clipboard.writeText(command);message.value='Command copied'}catch(error){message.value=error instanceof Error?error.message:'Copy failed'}}
</script>
<template><section aria-labelledby="install-title"><h2 id="install-title">Install Codex</h2><p class="muted">Download and run from this service. Uses CODEX_RELEASE if set, otherwise latest.</p><div class="two-columns"><article v-for="item in commands" :key="item.name" class="command"><div class="section-heading"><h3>{{item.name}}</h3><button type="button" @click="copy(item.command)" :aria-label="`Copy ${item.name} command`">Copy</button></div><pre><code>{{item.command}}</code></pre></article></div><p role="status">{{message}}</p><p class="muted">Installers verify hashes and suppress the automatic-update marker. The CLI binary is unchanged; runtime/API traffic requires your enterprise egress policy.</p></section></template>

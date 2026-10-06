(()=>{
const zh=document.documentElement.lang==='zh-CN';
const label=zh?'复制代码':'Copy code';
const status=document.createElement('span');status.className='copy-status';status.setAttribute('role','status');document.body.append(status);
for(const button of document.querySelectorAll('.copy-code')){
 button.textContent=zh?'复制':'Copy';button.setAttribute('aria-label',label);
 button.addEventListener('click',async()=>{
  const code=button.parentElement.querySelector('code');if(!code)return;
  try{await navigator.clipboard.writeText(code.textContent);status.textContent=zh?'已复制':'Copied';}
  catch{status.textContent=zh?'复制失败，请手动复制':'Copy failed; please copy manually';}
 });
}
})();

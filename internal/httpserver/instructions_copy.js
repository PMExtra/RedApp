(() => {
  const zh = document.documentElement.lang === 'zh-CN';
  for (const wrapper of document.querySelectorAll('[data-markdown-copy]')) {
    const button = wrapper.querySelector('.copy-code');
    const code = wrapper.querySelector('code');
    const status = wrapper.querySelector('.copy-status');
    if (!button || !code || !status) continue;
    const title = wrapper.querySelector('.copy-title');
    if (title) title.textContent = title.dataset.codeTitle || (zh ? '代码' : 'Code');
    const pre = wrapper.querySelector('pre');
    if (pre) { pre.tabIndex = 0; pre.setAttribute('aria-label', title.textContent); }
    const label = button.querySelector('span');
    label.textContent = zh ? '复制' : 'Copy';
    button.setAttribute('aria-label', zh ? '复制代码' : 'Copy code');
    button.addEventListener('click', async () => {
      if (button.disabled) return;
      button.disabled = true;
      status.textContent = '';
      status.hidden = true;
      status.removeAttribute('data-error');
      label.textContent = zh ? '复制中…' : 'Copying…';
      try {
        await navigator.clipboard.writeText(code.textContent);
        status.textContent = zh ? '已复制' : 'Copied';
      } catch {
        status.setAttribute('data-error', '');
        status.textContent = zh ? '复制失败，请手动选择并复制代码' : 'Copy failed; select and copy the code manually';
      } finally {
        status.hidden = false;
        button.disabled = false;
        label.textContent = zh ? '复制' : 'Copy';
      }
    });
  }
})();

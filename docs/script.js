const copyText = {
  config: `context: development
namespace: default
forwards:
  - service: postgres
    namespace: data
    ports:
      - "15432:5432"
  - service: redis
    ports:
      - "16379:6379"`,
  install: 'brew install opcode0x/tap/kubepin',
  run: 'kubepin'
};

document.querySelectorAll('[data-copy]').forEach((button) => {
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(copyText[button.dataset.copy]);
      const previous = button.textContent;
      button.textContent = 'Copied!';
      window.setTimeout(() => { button.textContent = previous; }, 1800);
    } catch {
      button.textContent = 'Select text to copy';
      window.setTimeout(() => { button.textContent = 'Copy'; }, 1800);
    }
  });
});

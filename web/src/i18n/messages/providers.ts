export const en = {
  template: "Configuration template (new profile only)",
  chooseTemplate: "Choose a template…",
  general: "Z.ai GLM-5.3 · General API (recommended)",
  coding: "Z.ai GLM-5.3 · Coding Plan (reference)",
  introduction: "Optional: prefill connection fields, then enter a profile name and your API key. Your key, proxy, and name are kept. Selection does not save or activate a profile. Save when ready, then activate separately. The model remains editable.",
  generalHelp: "Uses the General API endpoint and API billing, separate from Coding Plan subscription quota. Confirm model access and billing for your Z.ai account.",
  codingHelp: "Reference only: Coding Plan uses a separate endpoint and subscription quota. Z.ai restricts it to officially supported tools; ARTIFEX is not listed. Obtain Z.ai authorization before using this endpoint with ARTIFEX.",
  modelHelp: "GLM-5.3 is text-only with a 1M-token context. Reasoning must stay enabled; supported effort levels are low, high, and max. This template selects max and leaves the output limit at 0 (provider default). Review these settings if you change the model.",
  officialModel: "GLM-5.3 model guide",
  officialPolicy: "Coding Plan usage policy",
};

export const zh: Record<keyof typeof en, string> = {
  template: "設定範本（僅限新設定檔）",
  chooseTemplate: "選擇範本…",
  general: "Z.ai GLM-5.3 · General API（建議）",
  coding: "Z.ai GLM-5.3 · Coding Plan（參考）",
  introduction: "選填：預先填入連線欄位，再輸入設定檔名稱與您的 API key。您的 key、proxy 與名稱皆會保留。選取範本並不會儲存或啟用設定檔。準備就緒後請儲存，再另行啟用。模型仍可編輯。",
  generalHelp: "使用 General API 端點與 API 計費，與 Coding Plan 訂閱額度分開。請確認您 Z.ai 帳戶的模型存取權與計費方式。",
  codingHelp: "僅供參考：Coding Plan 使用獨立的端點與訂閱額度。Z.ai 僅允許官方支援的工具使用；ARTIFEX 未列於其中。在將此端點與 ARTIFEX 搭配使用前，請先取得 Z.ai 的授權。",
  modelHelp: "GLM-5.3 僅支援文字，具備 1M token 上下文。Reasoning 必須保持啟用；支援的 effort 等級為 low、high 與 max。此範本選擇 max，並將輸出上限設為 0（供應商預設值）。若您變更模型，請重新檢視這些設定。",
  officialModel: "GLM-5.3 模型指南",
  officialPolicy: "Coding Plan 使用政策",
};

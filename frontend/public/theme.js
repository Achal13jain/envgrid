(function () {
  try {
    var t = localStorage.getItem("envgrid-theme");
    var dark = t === "dark" || (t !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
    var size = localStorage.getItem("envgrid-size");
    if (size === "100" || size === "80") document.documentElement.style.fontSize = size + "%";
  } catch (e) {
    /* storage blocked: fall back to light */
  }
})();

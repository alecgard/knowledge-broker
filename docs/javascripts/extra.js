// Make the header title a link to the docs home page.
document.addEventListener("DOMContentLoaded", function () {
  var title = document.querySelector(".md-header__title");
  if (title) {
    title.addEventListener("click", function () {
      window.location = "/";
    });
  }
});

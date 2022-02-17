'use strict';

{{ $searchDataFile := printf "%s.search-data.json" .Language.Lang }}
{{ $searchData := resources.Get "search-data.json" | resources.ExecuteAsTemplate $searchDataFile . | resources.Minify | resources.Fingerprint }}
{{ $searchConfig := i18n "bookSearchConfig" | default "{}" }}

(function () {
  const searchDataURL = '{{ $searchData.RelPermalink }}';
  const indexConfig = Object.assign({{ $searchConfig }}, {
    id: 'id',
    tag: 'first',
    index: [
      {
        field: "title",
        tokenize: "forward",
        optimize: true,
        resolution: 9
      }, {
        field:  "content",
        tokenize: "strict",
        optimize: true,
        resolution: 5,
        minlength: 3,
        context: {
            depth: 1,
            resolution: 3
        }
      }],
    store: ['title', 'href', 'section', 'first']
  });

  const input = document.querySelector('#book-search-input');
  const results = document.querySelector('#book-search-results');

  if (!input) {
    return
  }

  input.addEventListener('focus', init);
  input.addEventListener('keyup', search);

  document.addEventListener('keypress', focusSearchFieldOnKeyPress);

  /**
   * @param {Event} event
   */
  function focusSearchFieldOnKeyPress(event) {
    if (event.target.value !== undefined) {
      return;
    }

    if (input === document.activeElement) {
      return;
    }

    const characterPressed = String.fromCharCode(event.charCode);
    if (!isHotkey(characterPressed)) {
      return;
    }

    input.focus();
    event.preventDefault();
  }

  /**
   * @param {String} character
   * @returns {Boolean} 
   */
  function isHotkey(character) {
    const dataHotkeys = input.getAttribute('data-hotkeys') || '';
    return dataHotkeys.indexOf(character) >= 0;
  }

  function init() {
    input.removeEventListener('focus', init); // init once
    input.required = true;

    fetch(searchDataURL)
      .then(pages => pages.json())
      .then(pages => {
        window.bookSearchIndex = new FlexSearch.Document(indexConfig);
        pages.forEach(function(page){
          window.bookSearchIndex.add(page);
        })
      })
      .then(() => input.required = false)
      .then(search);
  }

  function search() {
    while (results.firstChild) {
      results.removeChild(results.firstChild);
    }

    if (!input.value) {
      return;
    }

    const maxSearchResults = 10;
    var searchGroups = window.bookSearchIndex.search({
      index: ["title", "content"],
      query: input.value,
      tag: input.dataset.filter,
      limit: maxSearchResults + 2
    });

    let searchHits = [];
    let seen = {};
    searchGroups.forEach(function(group) {
      group.result.forEach(function(pageid) {
        if(seen[pageid]) return;
        seen[pageid] = true;
        searchHits.push(window.bookSearchIndex.store[pageid]);
      })
    })

    const hasMore = searchHits.length > maxSearchResults;
    searchHits = searchHits.slice(0, maxSearchResults);

    searchHits.forEach(function (page) {
      const li = element('<li><a href></a><small></small></li>');
      const a = li.querySelector('a'), small = li.querySelector('small');

      a.href = page.href;
      a.textContent = page.title;
      small.textContent = page.first + page.section;

      results.appendChild(li);
    });

    if(hasMore) {
      const li = element('<li><center>• • •</center></li>');
      results.appendChild(li);
    }
  }

  /**
   * @param {String} content
   * @returns {Node}
   */
  function element(content) {
    const div = document.createElement('div');
    div.innerHTML = content;
    return div.firstChild;
  }
})();

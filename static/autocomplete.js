(function (window, $) {
    'use strict';

    var instanceSeq = 0;

    function AutoComplete(options) {
        this.options = options || {};
        this.name = this.options.name || ('autocomplete_' + (++instanceSeq));
        this.inputSelector = this.options.inputSelector;
        this.hiddenSelector = this.options.hiddenSelector || '.autocomplete-id';
        this.datalistSelector = this.options.datalistSelector;
        this.items = [];
        this.byId = new Map();
        this.byDisplay = new Map();
        this.getId = this.options.getId || function (item) { return item.id; };
        this.getDisplay = this.options.getDisplay || function (item) { return String(item.name || ''); };
        this.getAliases = this.options.getAliases || function () { return []; };
        this.onSelect = this.options.onSelect || function () {};
        this.onClear = this.options.onClear || function () {};
        this.sortItems = this.options.sortItems || null;
        this.bindEvents();
    }

    AutoComplete.prototype.bindEvents = function () {
        var self = this;
        var namespace = '.ac_' + this.name;

        $(document)
            .off('input' + namespace, this.inputSelector)
            .on('input' + namespace, this.inputSelector, function () {
                var input = $(this);
                var hidden = self.hiddenFor(input);
                hidden.val('');
                input.removeClass('is-invalid');
                self.onClear(null, input);
            })
            .off('change' + namespace, this.inputSelector)
            .on('change' + namespace, this.inputSelector, function () {
                self.resolveInput($(this));
            });
    };

    AutoComplete.prototype.hiddenFor = function (input) {
        var row = input.closest('.row, tr, .autocomplete-row');
        var hidden = row.find(this.hiddenSelector).first();
        if (hidden.length) return hidden;

        var inputID = input.attr('id');
        if (inputID) {
            var byFor = $('#' + inputID + '-id');
            if (byFor.length) return byFor;
        }
        return input.siblings(this.hiddenSelector).first();
    };

    AutoComplete.prototype.setItems = function (items) {
        this.items = Array.isArray(items) ? items.slice() : [];
        this.byId = new Map();
        this.byDisplay = new Map();

        var self = this;
        this.items.forEach(function (item) {
            var id = String(self.getId(item));
            self.byId.set(id, item);
            self.byDisplay.set(self.getDisplay(item), item);
        });

        this.populateDatalist();
        this.syncBoundRows();
    };

    AutoComplete.prototype.populateDatalist = function () {
        var datalist = $(this.datalistSelector).first();
        if (!datalist.length) return;

        var self = this;
        var items = this.items.slice();
        if (this.sortItems) {
            items.sort(this.sortItems);
        }

        datalist.empty();
        items.forEach(function (item) {
            datalist.append($('<option>').val(self.getDisplay(item)));
        });
    };

    AutoComplete.prototype.syncBoundRows = function () {
        var self = this;
        $(this.inputSelector).each(function () {
            var input = $(this);
            var hidden = self.hiddenFor(input);
            var id = String(hidden.val() || '').trim();
            if (!id) return;

            var item = self.byId.get(id);
            if (!item) {
                hidden.val('');
                input.val('').removeClass('is-invalid');
                self.onClear(null, input);
                return;
            }
            input.val(self.getDisplay(item)).removeClass('is-invalid');
            self.onSelect(item, input);
        });
    };

    AutoComplete.prototype.findByAliases = function (value) {
        var self = this;
        var matches = this.items.filter(function (item) {
            var aliases = self.getAliases(item);
            if (!Array.isArray(aliases)) aliases = [aliases];
            return aliases.some(function (alias) {
                return String(alias || '').trim() === value;
            });
        });
        return matches.length === 1 ? matches[0] : null;
    };

    AutoComplete.prototype.resolveInput = function (input) {
        input = $(input).first();
        if (!input.length) return null;

        var hidden = this.hiddenFor(input);
        var value = String(input.val() || '').trim();

        if (!value) {
            hidden.val('');
            input.removeClass('is-invalid');
            this.onClear(null, input);
            return null;
        }

        var item = this.byDisplay.get(value) || this.findByAliases(value);
        if (!item) {
            hidden.val('');
            input.addClass('is-invalid');
            this.onClear(null, input);
            return null;
        }

        hidden.val(this.getId(item));
        input.val(this.getDisplay(item)).removeClass('is-invalid');
        this.onSelect(item, input);
        return item;
    };

    AutoComplete.prototype.resolveRow = function (row) {
        return this.resolveInput($(row).find(this.inputSelector).first());
    };

    AutoComplete.prototype.getItemById = function (id) {
        return this.byId.get(String(id || '')) || null;
    };

    window.AutoComplete = {
        create: function (options) {
            return new AutoComplete(options);
        }
    };
})(window, window.jQuery);
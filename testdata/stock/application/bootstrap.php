<?php

define('DOCROOT', realpath(dirname(__FILE__).'/..').DIRECTORY_SEPARATOR);
define('APPPATH', realpath(dirname(__FILE__)).DIRECTORY_SEPARATOR);
define('MODPATH', realpath(dirname(__FILE__).'/../modules').DIRECTORY_SEPARATOR);
define('SYSPATH', realpath(dirname(__FILE__).'/../system').DIRECTORY_SEPARATOR);

Kohana::modules([
    // No modules enabled in the stock fixture.
]);

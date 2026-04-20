<?php defined('SYSPATH') or die('No direct script access.');

class Model_Post extends ORM {
    protected $_table_name = 'posts';
    protected $_belongs_to = ['user' => []];
}
